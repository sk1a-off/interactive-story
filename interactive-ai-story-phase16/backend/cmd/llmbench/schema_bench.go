package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	storyllm "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/openai_compatible"
	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

type schemaProbe struct {
	ID       string
	Request  ai.StoryRequest
	Expected string
}
type schemaProbeCapture struct {
	ai.StoryLLM
	mu       sync.Mutex
	requests map[string]ai.StoryRequest
}

func (c *schemaProbeCapture) Generate(ctx context.Context, r ai.StoryRequest) (ai.StoryResponse, error) {
	c.mu.Lock()
	c.requests[r.Role] = r
	c.mu.Unlock()
	return c.StoryLLM.Generate(ctx, r)
}
func schemaProbes() ([]schemaProbe, error) {
	f := turnFixtures()[0]
	capture := &schemaProbeCapture{StoryLLM: fakeTurnModel(f), requests: map[string]ai.StoryRequest{}}
	gid, _ := id.New()
	pipeline := appgen.Pipeline{LLM: capture, Planner: appgen.LLMTurnPlanner{LLM: capture}, Canon: &turnCaptureCanon{}, Targets: preparedTurnFixture{Fixture: f}}
	if _, err := pipeline.Run(context.Background(), gid, appgen.PlayerAction{Text: f.Action, TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000008010"))}); err != nil {
		return nil, err
	}
	var probes []schemaProbe
	for _, role := range []string{"turn_planner", "writer", "choices"} {
		probes = append(probes, schemaProbe{ID: role, Request: capture.requests[role]})
	}
	for _, role := range []string{"world_evaluator", "state_evaluator", "quest_evaluator"} {
		for _, expected := range []string{"positive", "negative"} {
			request := capture.requests[role]
			var input map[string]any
			if json.Unmarshal(request.Input, &input) != nil {
				return nil, errors.New("invalid frozen role input")
			}
			beat := "Мира передала Илье латунный ключ, и он положил его в карман. Мира попросила найти пропавшего архивиста, и Илья согласился помочь. У двери стоял хранитель Северин, старый знакомый Миры."
			if expected == "negative" {
				beat = "Илья остался неподвижен. Мира молчала. Новых сведений не появилось."
			}
			input["newBeat"], input["action"], input["intent"] = beat, "Оглядеться и выслушать Миру.", "Оглядеться и выслушать Миру."
			input["bootstrapJournal"] = false
			request.Input, _ = json.Marshal(input)
			probes = append(probes, schemaProbe{ID: role + "/" + expected, Request: request, Expected: expected})
		}
	}
	return probes, nil
}

// These are narrow shape/evidence checks, not a complete JSON Schema engine or
// a substitute for Canon validators and human literary review.
func checkSchemaProbe(p schemaProbe, raw []byte) string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return "not_json_object"
	}
	stringField := func(k string) bool {
		var s string
		return json.Unmarshal(obj[k], &s) == nil && strings.TrimSpace(s) != ""
	}
	switch p.Request.Role {
	case "writer":
		if !stringField("text") {
			return "missing_text"
		}
	case "turn_planner":
		if !stringField("intent") || !stringField("immediateGoal") {
			return "missing_plan"
		}
		var transition string
		_ = json.Unmarshal(obj["transition"], &transition)
		if transition != "continue_scene" && transition != "new_scene" && transition != "new_chapter" {
			return "invalid_transition"
		}
	case "choices":
		var rows []struct{ ID, Label string }
		if json.Unmarshal(obj["choices"], &rows) != nil || len(rows) != 4 {
			return "not_four_choices"
		}
		ids, labels := map[string]bool{}, map[string]bool{}
		for _, r := range rows {
			if r.ID == "" || strings.TrimSpace(r.Label) == "" || ids[r.ID] || labels[strings.TrimSpace(r.Label)] {
				return "invalid_or_duplicate_choices"
			}
			ids[r.ID] = true
			labels[strings.TrimSpace(r.Label)] = true
		}
	default:
		var rows []struct {
			EvidenceQuote string `json:"evidenceQuote"`
		}
		if string(obj["changes"]) == "null" || json.Unmarshal(obj["changes"], &rows) != nil {
			return "missing_changes_array"
		}
		if p.Expected == "negative" && len(rows) > 0 {
			return "unsupported_negative_mutation"
		}
		if p.Expected == "positive" && len(rows) == 0 {
			return "missed_positive_mutation"
		}
		var input struct {
			NewBeat string `json:"newBeat"`
		}
		_ = json.Unmarshal(p.Request.Input, &input)
		for _, r := range rows {
			if strings.TrimSpace(r.EvidenceQuote) == "" || !strings.Contains(input.NewBeat, r.EvidenceQuote) {
				return "invalid_evidence_quote"
			}
		}
	}
	return ""
}

type schemaBenchRow struct {
	Case        string       `json:"case"`
	Repeat      int          `json:"repeat"`
	Schema      bool         `json:"schema"`
	InputSHA256 string       `json:"inputSha256"`
	Output      string       `json:"output"`
	Passed      bool         `json:"passed"`
	Failure     string       `json:"failure,omitempty"`
	Call        turnRoleCall `json:"call"`
}
type schemaBenchSummary struct {
	Case                               string `json:"case"`
	Schema                             bool   `json:"schema"`
	Runs, Passed, Fallbacks, HTTPPosts int
	Latency                            turnTimings `json:"latency"`
}
type schemaBenchReport struct {
	SchemaSHA256          string `json:"schemaSha256"`
	Status                string `json:"status"`
	Provider              string `json:"provider"`
	ConfigID              id.ID  `json:"configId"`
	ConfigRevision        int64  `json:"configRevision"`
	PromptSHA256          string `json:"promptSha256"`
	FixtureSHA256         string `json:"fixtureSha256"`
	StartedAt, FinishedAt time.Time
	Repeats               int                  `json:"repeats"`
	Concurrency           int                  `json:"concurrency"`
	Seed                  int64                `json:"seed"`
	Runs                  []schemaBenchRow     `json:"runs"`
	Summary               []schemaBenchSummary `json:"summary"`
	Notes                 []string             `json:"notes"`
}

func summarizeSchemaBench(rows []schemaBenchRow) []schemaBenchSummary {
	by := map[string]*schemaBenchSummary{}
	times := map[string][]float64{}
	for _, r := range rows {
		key := fmt.Sprintf("%s/%t", r.Case, r.Schema)
		s := by[key]
		if s == nil {
			s = &schemaBenchSummary{Case: r.Case, Schema: r.Schema}
			by[key] = s
		}
		s.Runs++
		if r.Passed {
			s.Passed++
			times[key] = append(times[key], r.Call.ElapsedMS)
		}
		if r.Call.SchemaFallback {
			s.Fallbacks++
		}
		s.HTTPPosts += r.Call.HTTPPosts
	}
	var out []schemaBenchSummary
	for k, s := range by {
		s.Latency = turnDistribution(times[k])
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Case != out[j].Case {
			return out[i].Case < out[j].Case
		}
		return !out[i].Schema && out[j].Schema
	})
	return out
}
func runSchemaBenchmark(o turnBenchmarkOptions, output string) (runErr error) {
	if output == "" || o.Repeats < 1 || o.Repeats > 20 || (o.Provider != "configured" && o.Provider != "fake") {
		return errors.New("schema benchmark requires output, configured/fake provider and 1..20 repeats")
	}
	root, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(root, 40*time.Minute)
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
	}
	probes, err := schemaProbes()
	if err != nil {
		return errors.New("schema fixtures unavailable")
	}
	hash := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	pr, _ := json.Marshal(prompt)
	fr, _ := json.Marshal(probes)
	report := schemaBenchReport{SchemaSHA256: storyllm.ResponseSchemaFingerprint(), Status: "running", Provider: o.Provider, ConfigID: rev.ID, ConfigRevision: rev.Revision, PromptSHA256: hash(pr), FixtureSHA256: hash(fr), StartedAt: time.Now().UTC(), Repeats: o.Repeats, Concurrency: 2, Seed: o.Seed, Notes: []string{
		"Frozen synthetic role inputs captured from the production pipeline with a scripted writer. Provider configuration is read-only; no user stories, database fixtures or Canon writes.",
		"Two concurrent paired workers; schema/object order alternates per case across repeats. Logical provider usage can omit rejected calls. Physical requests and effective formats are retained.",
		"Checks cover required core fields, four unique choices, nonempty positive mutations, no negative mutations and verbatim beat evidence. These are not complete semantic or literary acceptance.",
		"Role latency excludes the full pipeline, queue, SQL commits and browser rendering. Successful shape/evidence checks alone do not establish a speed or quality improvement.",
	}}
	var mu sync.Mutex
	save := func() error {
		report.Summary = summarizeSchemaBench(report.Runs)
		b, e := json.MarshalIndent(report, "", "  ")
		if e != nil {
			return e
		}
		return os.WriteFile(output, append(b, '\n'), 0600)
	}
	defer func() {
		report.FinishedAt = time.Now().UTC()
		if err := save(); err != nil {
			runErr = errors.New("schema report write failed")
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
				arms := []bool{false, true}
				if (task.index+task.repeat)%2 == 1 {
					arms = []bool{true, false}
				}
				for _, enabled := range arms {
					if ctx.Err() != nil {
						return
					}
					options := o
					options.JSONSchema = enabled
					model, e := turnBenchmarkModel(options, rev, prompt, keys, turnFixtures()[0])
					if e != nil {
						cancel()
						return
					}
					if o.Provider == "fake" && p.Expected == "positive" {
						change := map[string]any{"evidenceQuote": "У двери стоял хранитель Северин, старый знакомый Миры."}
						switch p.Request.Role {
						case "world_evaluator":
							change["type"], change["name"] = "upsert_character", "Северин"
						case "state_evaluator":
							change["operation"], change["category"], change["name"], change["quantity"] = "create", "item", "Ключ", 1
							change["evidenceQuote"] = "Мира передала Илье латунный ключ, и он положил его в карман."
						case "quest_evaluator":
							change["operation"], change["scope"], change["kind"], change["title"], change["successCriteria"], change["status"], change["progress"] = "create", "global", "quest", "Найти архивиста", "Архивист найден", "active", 0
							change["evidenceQuote"] = "Мира попросила найти пропавшего архивиста, и Илья согласился помочь."
						}
						body, _ := json.Marshal(map[string]any{"changes": []any{change}})
						model.(*fakeai.ScriptedStoryLLM).Responses[p.Request.Role] = body
					}
					// A fresh pair arm makes rejection/fallback costs visible per request.
					observer := &turnObserver{base: model, started: time.Now()}
					response, e := observer.Generate(ctx, p.Request)
					row := schemaBenchRow{Case: p.ID, Repeat: task.repeat + 1, Schema: enabled, InputSHA256: hash(p.Request.Input), Output: string(response.Output), Call: observer.calls[0]}
					if e != nil {
						row.Failure = "provider_call_failed"
					} else {
						row.Failure = checkSchemaProbe(p, response.Output)
					}
					row.Passed = row.Failure == ""
					mu.Lock()
					report.Runs = append(report.Runs, row)
					saveErr := save()
					mu.Unlock()
					fmt.Printf("schema case=%s repeat=%d enabled=%t passed=%t elapsed_ms=%.0f posts=%d format=%s fallback=%t\n", row.Case, row.Repeat, enabled, row.Passed, row.Call.ElapsedMS, row.Call.HTTPPosts, row.Call.ResponseFormat, row.Call.SchemaFallback)
					if saveErr != nil {
						cancel()
						return
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
	report.Status = "measured"
	if len(report.Runs) != len(probes)*o.Repeats*2 {
		report.Status = "failed"
	}
	for _, r := range report.Runs {
		if !r.Passed {
			report.Status = "failed"
		}
	}
	if report.Status == "failed" {
		return errors.New("schema benchmark has failed or incomplete cases; inspect saved report")
	}
	if o.Provider == "fake" {
		report.Status = "smoke_only"
	}
	return nil
}
