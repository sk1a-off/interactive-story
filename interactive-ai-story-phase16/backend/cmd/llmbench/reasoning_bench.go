package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	storyllm "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/openai_compatible"
	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func reasoningProbes(fastState bool) ([]schemaProbe, error) {
	source, err := schemaProbes()
	if err != nil {
		return nil, err
	}
	var out []schemaProbe
	for _, p := range source {
		if p.Request.Role == "turn_planner" {
			p.ID = "planner/conversation"
			out = append(out, p)
			for _, f := range turnFixtures()[1:] {
				var input map[string]any
				_ = json.Unmarshal(p.Request.Input, &input)
				input["action"] = f.Action
				r := p.Request
				r.Input, _ = json.Marshal(input)
				out = append(out, schemaProbe{ID: "planner/" + f.ID, Request: r})
			}
		}
		if p.Request.Role == "choices" {
			out = append(out, p)
		}
		if p.Request.Role == "state_evaluator" && fastState {
			out = append(out, p)
			if p.Expected == "negative" {
				for _, c := range []struct{ id, text string }{
					{"state/intention", "Илья собирался забрать серебряный жетон, но дверь осталась запертой. Он ничего не получил."},
					{"state/observation", "За стеклом лежал латунный ключ. Илья лишь посмотрел на него и отошёл."},
				} {
					var input map[string]any
					_ = json.Unmarshal(p.Request.Input, &input)
					input["newBeat"] = c.text
					r := p.Request
					r.Input, _ = json.Marshal(input)
					out = append(out, schemaProbe{ID: c.id, Request: r, Expected: "negative"})
				}
			}
		}
	}
	return out, nil
}

type probeReplyLLM struct {
	output []byte
	effort string
}

func (f probeReplyLLM) Identity() ai.ProviderIdentity {
	return ai.ProviderIdentity{Kind: ai.KindStoryLLM, Provider: "fake", Model: "reasoning-mechanical-only"}
}
func (f probeReplyLLM) Generate(context.Context, ai.StoryRequest) (ai.StoryResponse, error) {
	return ai.StoryResponse{Output: f.output, ReasoningEffort: f.effort}, nil
}
func fakeReasoningReply(p schemaProbe, effort string) ai.StoryLLM {
	var output []byte
	switch p.Request.Role {
	case "turn_planner":
		var input struct{ Action string }
		_ = json.Unmarshal(p.Request.Input, &input)
		output, _ = json.Marshal(map[string]any{"intent": input.Action, "immediateGoal": input.Action, "transition": "continue_scene"})
	case "choices":
		output = []byte(`{"choices":[{"id":"ask","label":"Спросить Миру"},{"id":"map","label":"Изучить карту"},{"id":"listen","label":"Прислушаться"},{"id":"wait","label":"Подождать"}]}`)
	default:
		output = []byte(`{"changes":[]}`)
		if p.Expected == "positive" {
			output = []byte(`{"changes":[{"operation":"create","category":"item","name":"Латунный ключ","quantity":1,"status":"active","evidenceQuote":"Мира передала Илье латунный ключ, и он положил его в карман."}]}`)
		}
	}
	return probeReplyLLM{output: output, effort: effort}
}
func checkReasoningProbe(p schemaProbe, output []byte) (string, int) {
	if why := checkSchemaProbe(p, output); why != "" {
		return why, 0
	}
	if p.Request.Role == "state_evaluator" {
		r := replaySchemaEvaluator(p, schemaBenchRow{Case: p.ID, Output: string(output)})
		return r.Failure, r.Mutations
	}
	if p.Request.Role == "turn_planner" {
		var input struct{ Action string }
		_ = json.Unmarshal(p.Request.Input, &input)
		plan, err := (appgen.LLMTurnPlanner{LLM: probeReplyLLM{output: output}}).Plan(context.Background(), appgen.PlayerAction{Text: input.Action}, turnFixtures()[0].Target, nil)
		if err != nil {
			return "planner_validation_failed", 0
		}
		if plan.IntentFallbackUsed {
			return "planner_required_agency_fallback", 0
		}
	}
	return "", 0
}

type reasoningBenchRow struct {
	Case        string       `json:"case"`
	Repeat      int          `json:"repeat"`
	Effort      string       `json:"effort"`
	InputSHA256 string       `json:"inputSha256"`
	Output      string       `json:"output"`
	Passed      bool         `json:"passed"`
	Failure     string       `json:"failure,omitempty"`
	Mutations   int          `json:"mutations"`
	Call        turnRoleCall `json:"call"`
}
type reasoningBenchSummary struct {
	Case                                                      string `json:"case"`
	Effort                                                    string `json:"effort"`
	Runs, Passed, ProviderFailed, ValidationFailed, HTTPPosts int
	ReasoningUsageReported                                    int `json:"reasoningUsageReported"`
	InputTokens, OutputTokens, ReportedReasoningTokens        int64
	Latency                                                   turnTimings `json:"latency"`
}
type reasoningBenchReport struct {
	Status                string `json:"status"`
	AcceptancePassed      bool   `json:"acceptancePassed"`
	Provider              string `json:"provider"`
	Model                 string `json:"model"`
	ConfigID              id.ID  `json:"configId"`
	ConfigRevision        int64  `json:"configRevision"`
	PromptSHA256          string `json:"promptSha256"`
	FixtureSHA256         string `json:"fixtureSha256"`
	StartedAt, FinishedAt time.Time
	Repeats               int                     `json:"repeats"`
	Concurrency           int                     `json:"concurrency"`
	Seed                  int64                   `json:"seed"`
	Runs                  []reasoningBenchRow     `json:"runs"`
	Summary               []reasoningBenchSummary `json:"summary"`
	Notes                 []string                `json:"notes"`
}

func summarizeReasoningRows(rows []reasoningBenchRow) []reasoningBenchSummary {
	by := map[string]*reasoningBenchSummary{}
	times := map[string][]float64{}
	for _, r := range rows {
		key := r.Case + "/" + r.Effort
		s := by[key]
		if s == nil {
			s = &reasoningBenchSummary{Case: r.Case, Effort: r.Effort}
			by[key] = s
		}
		s.Runs++
		if r.Passed {
			s.Passed++
		}
		if r.Call.Failed {
			s.ProviderFailed++
		} else if !r.Passed {
			s.ValidationFailed++
		}
		// Failed requests retain their elapsed times; no survivor-only timing summary.
		times[key] = append(times[key], r.Call.ElapsedMS)
		s.HTTPPosts += r.Call.HTTPPosts
		s.InputTokens += r.Call.InputTokens
		s.OutputTokens += r.Call.OutputTokens
		if r.Call.ReasoningTokens != nil {
			s.ReasoningUsageReported++
			s.ReportedReasoningTokens += *r.Call.ReasoningTokens
		}
	}
	var result []reasoningBenchSummary
	for key, s := range by {
		s.Latency = turnDistribution(times[key])
		result = append(result, *s)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Case != result[j].Case {
			return result[i].Case < result[j].Case
		}
		return result[i].Effort < result[j].Effort
	})
	return result
}
func runReasoningBenchmark(o turnBenchmarkOptions, output string) (runErr error) {
	if output == "" || o.Repeats < 1 || o.Repeats > 100 || (o.Provider != "configured" && o.Provider != "fake") {
		return errors.New("reasoning benchmark requires output, configured/fake provider and 1..100 repeats")
	}
	root, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(root, 20*time.Minute)
	defer cancel()
	var rev aiconfig.Revision
	var prompts promptset.Revision
	var keys []string
	var err error
	if o.Provider == "configured" {
		if o.ConfigDSN == "" {
			return errors.New("read-only configuration database required")
		}
		rev, prompts, keys, err = turnBenchmarkConfigured(ctx, o)
		if err != nil {
			return err
		}
		var settings aiconfig.PublicSettings
		_ = json.Unmarshal(rev.Settings, &settings)
		if rev.StoryLLM.Provider != "google_gemini" || (!settings.StoryLLMSafeHybrid && rev.StoryLLM.Model != aiconfig.Gemini35FlashLiteModel) {
			return errors.New("benchmark requires the current Gemini 3.5 Flash-Lite route; no automatic model substitution")
		}
	}
	probes, err := reasoningProbes(o.FastState)
	if err != nil {
		return err
	}
	if o.Cases != "" {
		var selected []schemaProbe
		for _, p := range probes {
			for _, c := range strings.Split(o.Cases, ",") {
				if p.ID == strings.TrimSpace(c) {
					selected = append(selected, p)
					break
				}
			}
		}
		probes = selected
	}
	if len(probes) == 0 {
		return errors.New("no matching reasoning cases")
	}
	hash := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	fr, _ := json.Marshal(probes)
	report := reasoningBenchReport{Status: "running", Provider: o.Provider, Model: aiconfig.Gemini35FlashLiteModel, ConfigID: rev.ID, ConfigRevision: rev.Revision, PromptSHA256: promptFingerprint(prompts), FixtureSHA256: hash(fr), StartedAt: time.Now().UTC(), Repeats: o.Repeats, Concurrency: 2, Seed: o.Seed, Notes: []string{
		"Frozen synthetic fast-role inputs; configuration and credential store are read-only. No database fixtures, Canon SQL writes or active-setting changes.",
		"Paired low/minimal requests share exact inputs, prompt revision, model and JSON-object format. Two concurrent workers; within-case order reverses each repeat.",
		"Direct current Flash-Lite route, with no Safe Hybrid quality fallback to hide unsupported settings or inflate a different model's timing. Requests are capped at 60 seconds.",
		"Checks include exact-four distinct choices, planner agency/transition validation and production Pipeline/Canon replay for state mutations. Not a general literary or continuity quality gate.",
		"Latency summaries include failed calls. Missing reasoning-token usage remains null/absent, not zero. Accepted wire parameters do not prove the provider used a specific internal thinking budget.",
		"Antigravity agent/writer reasoning depth is not exposed in the documented agent configuration and is not changed or benchmarked here. Full-turn queue/SQL/browser time is excluded.",
	}}
	var mu sync.Mutex
	var rejected atomic.Bool
	save := func() error {
		report.Summary = summarizeReasoningRows(report.Runs)
		raw, e := json.MarshalIndent(report, "", "  ")
		if e != nil {
			return e
		}
		return os.WriteFile(output, append(raw, '\n'), 0600)
	}
	defer func() {
		report.FinishedAt = time.Now().UTC()
		if err := save(); err != nil {
			runErr = errors.New("reasoning report write failed")
		}
	}()
	type task struct{ index, repeat int }
	tasks := make(chan task)
	var wg sync.WaitGroup
	for worker := 0; worker < 2; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range tasks {
				p := probes[task.index]
				arms := []string{"low", "minimal"}
				if (task.index+task.repeat)%2 == 1 {
					arms = []string{"minimal", "low"}
				}
				for _, effort := range arms {
					if ctx.Err() != nil {
						return
					}
					var model ai.StoryLLM
					if o.Provider == "fake" {
						model = fakeReasoningReply(p, effort)
					} else {
						var settings aiconfig.PublicSettings
						_ = json.Unmarshal(rev.Settings, &settings)
						built, buildErr := storyllm.New(storyllm.Config{BaseURL: settings.StoryLLMEndpoint, Provider: "google_gemini", Model: aiconfig.Gemini35FlashLiteModel, Profile: "current-fast-route-reasoning-probe", APIKeys: keys, PromptSet: &prompts, RoleReasoningEffort: map[string]string{p.Request.Role: effort}, Timeout: time.Minute}, &http.Client{Timeout: time.Minute, Transport: turnTransport{base: http.DefaultTransport}})
						if buildErr != nil {
							cancel()
							return
						}
						model = built
					}
					observer := &turnObserver{base: model, started: time.Now()}
					response, callErr := observer.Generate(ctx, p.Request)
					row := reasoningBenchRow{Case: p.ID, Repeat: task.repeat + 1, Effort: effort, InputSHA256: hash(p.Request.Input), Output: string(response.Output), Call: observer.calls[0]}
					if callErr != nil {
						row.Failure = "provider_call_failed"
					} else {
						row.Failure, row.Mutations = checkReasoningProbe(p, response.Output)
					}
					row.Passed = row.Failure == ""
					mu.Lock()
					report.Runs = append(report.Runs, row)
					saveErr := save()
					mu.Unlock()
					fmt.Printf("reasoning case=%s repeat=%d effort=%s passed=%t elapsed_ms=%.0f posts=%d\n", row.Case, row.Repeat, effort, row.Passed, row.Call.ElapsedMS, row.Call.HTTPPosts)
					if saveErr != nil {
						cancel()
						return
					}
					for _, status := range row.Call.HTTPStatuses {
						if status == 400 || status == 422 {
							rejected.Store(true)
							cancel()
							return
						}
					}
				}
			}
		}()
	}
	rng := rand.New(rand.NewSource(o.Seed))
dispatch:
	for repeat := 0; repeat < o.Repeats; repeat++ {
		for _, index := range rng.Perm(len(probes)) {
			select {
			case tasks <- task{index: index, repeat: repeat}:
			case <-ctx.Done():
				break dispatch
			}
		}
	}
	close(tasks)
	wg.Wait()
	if rejected.Load() {
		report.Status = "unsupported_request"
		return nil
	}
	if len(report.Runs) != len(probes)*o.Repeats*2 {
		report.Status = "interrupted"
		return errors.New("reasoning benchmark incomplete; saved partial report")
	}
	report.Status = "measured"
	report.AcceptancePassed = true
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
