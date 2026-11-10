package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	storyllm "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/openai_compatible"
	"github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

type cacheProbe struct {
	schemaProbe
	Variant string
}

func cacheProbes() ([]cacheProbe, error) {
	source, err := schemaProbes()
	if err != nil {
		return nil, err
	}
	previous := []string{turnFixtures()[0].Target.PreviousBeatText, "Илья убедился, что вода ещё не дошла до внутренней лестницы. Мира ждала у двери.", "Из-за двери донёсся стук. Мира отступила к стене и выжидательно посмотрела на Илью."}
	var probes []cacheProbe
	for _, p := range source {
		if p.Request.Role != "turn_planner" && p.Request.Role != "writer" {
			continue
		}
		for i, f := range turnFixtures() {
			var input map[string]json.RawMessage
			_ = json.Unmarshal(p.Request.Input, &input)
			var target map[string]json.RawMessage
			_ = json.Unmarshal(input["context"], &target)
			target["previousBeatText"], _ = json.Marshal(previous[i])
			target["nextBeatPosition"], _ = json.Marshal(4 + i)
			target["chapterBeatCount"], _ = json.Marshal(3 + i)
			input["context"], _ = json.Marshal(target)
			if p.Request.Role == "turn_planner" {
				input["action"], _ = json.Marshal(f.Action)
			} else {
				input["intent"], _ = json.Marshal(f.Action)
				input["goal"], _ = json.Marshal("Продолжить сцену с соблюдением выбранного действия.")
			}
			request := p.Request
			request.Input, _ = json.Marshal(input)
			probes = append(probes, cacheProbe{schemaProbe: schemaProbe{ID: p.Request.Role + "/" + f.ID, Request: request}, Variant: f.ID})
		}
	}
	return probes, nil
}
func cacheModel(o turnBenchmarkOptions, rev aiconfig.Revision, prompt promptset.Revision, role, key string, p cacheProbe) (ai.StoryLLM, error) {
	if o.Provider == "fake" {
		if role == "writer" {
			raw, _ := json.Marshal(map[string]string{"text": turnFixtures()[0].FakeText})
			return probeReplyLLM{output: raw}, nil
		}
		return fakeReasoningReply(p.schemaProbe, "low"), nil
	}
	var settings aiconfig.PublicSettings
	_ = json.Unmarshal(rev.Settings, &settings)
	model := rev.StoryLLM.Model
	if settings.StoryLLMSafeHybrid && role == "turn_planner" {
		model = aiconfig.Gemini35FlashLiteModel
	}
	return storyllm.New(storyllm.Config{BaseURL: settings.StoryLLMEndpoint, APIKeys: []string{key}, Model: model, Provider: rev.StoryLLM.Provider, Profile: "current-role-prefix-cache-probe", PromptSet: &prompt, Timeout: 2 * time.Minute}, &http.Client{Timeout: 2 * time.Minute, Transport: turnTransport{base: http.DefaultTransport}})
}

type cacheBenchRow struct {
	Case                  string       `json:"case"`
	Role                  string       `json:"role"`
	Layout                string       `json:"layout"`
	Repeat                int          `json:"repeat"`
	CredentialSlot        int          `json:"credentialSlot"`
	ExactInputRepeat      bool         `json:"exactInputRepeat"`
	SemanticSHA256        string       `json:"semanticSha256"`
	WireInputSHA256       string       `json:"wireInputSha256"`
	InputBytes            int          `json:"inputBytes"`
	SharedUserPrefixBytes int          `json:"sharedUserPrefixBytes"`
	Passed                bool         `json:"passed"`
	Failure               string       `json:"failure,omitempty"`
	Output                string       `json:"output"`
	Call                  turnRoleCall `json:"call"`
}
type cacheBenchSummary struct {
	Role                                                                                                        string `json:"role"`
	Layout                                                                                                      string `json:"layout"`
	Runs, Passed, Failed, CacheReported, CacheUnreported, PositiveCache, ReportedZero, DistinctSuccessfulInputs int
	ReportedCachedTokens                                                                                        int64
	Latency                                                                                                     turnTimings `json:"latency"`
}
type cacheBenchReport struct {
	Status                string `json:"status"`
	AcceptancePassed      bool   `json:"acceptancePassed"`
	Provider              string `json:"provider"`
	ConfigID              id.ID  `json:"configId"`
	ConfigRevision        int64  `json:"configRevision"`
	PromptSHA256          string `json:"promptSha256"`
	FixtureSHA256         string `json:"fixtureSha256"`
	StartedAt, FinishedAt time.Time
	Repeats               int                 `json:"repeats"`
	Preflight             []cacheBenchRow     `json:"preflight"`
	Runs                  []cacheBenchRow     `json:"runs"`
	Summary               []cacheBenchSummary `json:"summary"`
	UnavailableRoles      []string            `json:"unavailableRoles"`
	Notes                 []string            `json:"notes"`
}

func summarizeCacheRows(rows []cacheBenchRow) []cacheBenchSummary {
	by := map[string]*cacheBenchSummary{}
	times := map[string][]float64{}
	distinct := map[string]map[string]bool{}
	for _, r := range rows {
		key := r.Role + "/" + r.Layout
		s := by[key]
		if s == nil {
			s = &cacheBenchSummary{Role: r.Role, Layout: r.Layout}
			by[key] = s
			distinct[key] = map[string]bool{}
		}
		s.Runs++
		times[key] = append(times[key], r.Call.ElapsedMS)
		if !r.Passed {
			s.Failed++
			continue
		}
		s.Passed++
		distinct[key][r.WireInputSHA256] = true
		if r.Call.CachedInputTokens == nil {
			s.CacheUnreported++
		} else {
			s.CacheReported++
			s.ReportedCachedTokens += *r.Call.CachedInputTokens
			if *r.Call.CachedInputTokens > 0 {
				s.PositiveCache++
			} else {
				s.ReportedZero++
			}
		}
	}
	var summaries []cacheBenchSummary
	for key, s := range by {
		s.Latency = turnDistribution(times[key])
		s.DistinctSuccessfulInputs = len(distinct[key])
		summaries = append(summaries, *s)
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Role != summaries[j].Role {
			return summaries[i].Role < summaries[j].Role
		}
		return summaries[i].Layout < summaries[j].Layout
	})
	return summaries
}
func runCacheBenchmark(o turnBenchmarkOptions, output string) (runErr error) {
	if output == "" || o.Repeats < 1 || o.Repeats > 10 || (o.Provider != "configured" && o.Provider != "fake") {
		return errors.New("cache benchmark requires output, configured/fake provider and 1..10 repeats")
	}
	root, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(root, 25*time.Minute)
	defer cancel()
	var rev aiconfig.Revision
	var prompt promptset.Revision
	var keys []string
	var err error
	if o.Provider == "configured" {
		if o.ConfigDSN == "" {
			return errors.New("read-only configuration database required")
		}
		rev, prompt, keys, err = turnBenchmarkConfigured(ctx, o)
		if err != nil {
			return err
		}
		if rev.StoryLLM.Provider != "google_gemini" {
			return errors.New("cache measurement requires the current Google provider; no automatic model substitution")
		}
	}
	probes, err := cacheProbes()
	if err != nil {
		return err
	}
	if o.Cases != "" {
		var selected []cacheProbe
		for _, p := range probes {
			for _, name := range strings.Split(o.Cases, ",") {
				if strings.TrimSpace(name) == p.ID || strings.TrimSpace(name) == p.Request.Role {
					selected = append(selected, p)
					break
				}
			}
		}
		probes = selected
	}
	if len(probes) == 0 {
		return errors.New("no matching cache cases")
	}
	hash := func(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
	pr, _ := json.Marshal(prompt)
	fr, _ := json.Marshal(probes)
	report := cacheBenchReport{Status: "running", Provider: o.Provider, ConfigID: rev.ID, ConfigRevision: rev.Revision, PromptSHA256: hash(pr), FixtureSHA256: hash(fr), StartedAt: time.Now().UTC(), Repeats: o.Repeats, Notes: []string{
		"Experimental JSON member order only; production prompt ordering and active settings remain unchanged. Both layouts preserve identical JSON data and system instructions.",
		"Natural compact synthetic story contexts; no cache-threshold padding. Shared-prefix byte counts are not tokenizer counts or proof of cache eligibility.",
		"One request at a time. Credential probes retain all failures; a working credential is pinned per role and never rotated in measured requests. No quality-model escalation.",
		"Implicit cache is not reset or explicitly managed. Preflight can warm the current layout. Exact repeated inputs are marked separately; neither layout is guaranteed cold.",
		"Provider cached-token totals may include managed-agent internal work. Missing counters remain missing, not zero. Latency includes failed calls and is not a full-turn measurement.",
	}}
	save := func() error {
		report.Summary = summarizeCacheRows(report.Runs)
		b, e := json.MarshalIndent(report, "", "  ")
		if e != nil {
			return e
		}
		return os.WriteFile(output, append(b, '\n'), 0600)
	}
	defer func() {
		report.FinishedAt = time.Now().UTC()
		if err := save(); err != nil {
			runErr = errors.New("cache report write failed")
		}
	}()
	group := map[string][]cacheProbe{}
	var roles []string
	for _, p := range probes {
		if _, exists := group[p.Request.Role]; !exists {
			roles = append(roles, p.Request.Role)
		}
		group[p.Request.Role] = append(group[p.Request.Role], p)
	}
	seen := map[string]bool{}
	call := func(p cacheProbe, layout string, repeat, slot, prefix int, model ai.StoryLLM) (cacheBenchRow, error) {
		input := p.Request.Input
		if layout == "static_first" {
			var e error
			input, e = orderedPromptPrefix(input)
			if e != nil {
				return cacheBenchRow{}, e
			}
		}
		canonical, e := semanticPromptJSON(input)
		if e != nil {
			return cacheBenchRow{}, e
		}
		original, e := semanticPromptJSON(p.Request.Input)
		if e != nil || !bytes.Equal(canonical, original) {
			return cacheBenchRow{}, errors.New("prefix ordering changed semantic input")
		}
		request := p.Request
		request.Input = input
		wire := hash(input)
		seenKey := fmt.Sprintf("%s/%d/%s", p.Request.Role, slot, wire)
		row := cacheBenchRow{Case: p.ID, Role: p.Request.Role, Layout: layout, Repeat: repeat, CredentialSlot: slot, ExactInputRepeat: seen[seenKey], SemanticSHA256: hash(canonical), WireInputSHA256: wire, InputBytes: len(input), SharedUserPrefixBytes: prefix}
		seen[seenKey] = true
		observer := &turnObserver{base: model, started: time.Now()}
		response, callErr := observer.Generate(ctx, request)
		row.Call = observer.calls[0]
		row.Output = string(response.Output)
		if callErr != nil {
			row.Failure = "provider_call_failed"
		} else {
			row.Failure = checkSchemaProbe(p.schemaProbe, response.Output)
		}
		row.Passed = row.Failure == ""
		return row, nil
	}
	for _, role := range roles {
		cases := group[role]
		prefixes := map[string]int{}
		for _, layout := range []string{"current", "static_first"} {
			var inputs [][]byte
			for _, p := range cases {
				raw := p.Request.Input
				if layout == "static_first" {
					raw, err = orderedPromptPrefix(raw)
					if err != nil {
						return err
					}
				}
				inputs = append(inputs, raw)
			}
			prefixes[layout] = commonPromptPrefix(inputs)
		}
		var model ai.StoryLLM
		selectedSlot := -1
		if o.Provider == "fake" {
			model, _ = cacheModel(o, rev, prompt, role, "", cases[0])
			selectedSlot = 0
		} else {
			for i, key := range keys {
				model, err = cacheModel(o, rev, prompt, role, key, cases[0])
				if err != nil {
					return err
				}
				row, e := call(cases[0], "current", 0, i+1, prefixes["current"], model)
				if e != nil {
					return e
				}
				report.Preflight = append(report.Preflight, row)
				if err := save(); err != nil {
					return err
				}
				if row.Passed {
					selectedSlot = i + 1
					break
				}
				last := 0
				if len(row.Call.HTTPStatuses) > 0 {
					last = row.Call.HTTPStatuses[len(row.Call.HTTPStatuses)-1]
				}
				if last != 403 && last != 429 {
					break
				}
			}
		}
		if selectedSlot < 0 {
			report.UnavailableRoles = append(report.UnavailableRoles, role)
			continue
		}
		for repeat := 0; repeat < o.Repeats; repeat++ {
			for i, p := range cases {
				layouts := []string{"current", "static_first"}
				if (repeat+i)%2 == 1 {
					layouts = []string{"static_first", "current"}
				}
				for _, layout := range layouts {
					if ctx.Err() != nil {
						report.Status = "interrupted"
						return errors.New("cache benchmark interrupted; partial evidence saved")
					}
					if o.Provider == "fake" {
						model, _ = cacheModel(o, rev, prompt, role, "", p)
					}
					row, e := call(p, layout, repeat+1, selectedSlot, prefixes[layout], model)
					if e != nil {
						return e
					}
					report.Runs = append(report.Runs, row)
					if err := save(); err != nil {
						return err
					}
					fmt.Printf("cache case=%s repeat=%d layout=%s passed=%t elapsed_ms=%.0f cache_reported=%t\n", row.Case, row.Repeat, layout, row.Passed, row.Call.ElapsedMS, row.Call.CachedInputTokens != nil)
				}
			}
		}
	}
	if ctx.Err() != nil {
		report.Status = "interrupted"
		return errors.New("cache benchmark interrupted; partial evidence saved")
	}
	report.Status = "measured"
	if len(report.Runs) == 0 {
		report.Status = "unavailable"
	}
	report.AcceptancePassed = len(report.UnavailableRoles) == 0 && len(report.Runs) == len(probes)*o.Repeats*2
	for _, row := range report.Runs {
		if !row.Passed {
			report.AcceptancePassed = false
		}
	}
	if o.Provider == "fake" {
		report.Status = "smoke_only"
	}
	return nil
}
