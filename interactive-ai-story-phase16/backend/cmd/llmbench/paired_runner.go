package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

type pairedFormatKey struct{}

type pairedTransport struct{ base http.RoundTripper }

func (t pairedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host == "127.0.0.1:8791" {
		r = r.Clone(r.Context())
		r.Header = r.Header.Clone()
		r.Header.Set("X-Story-Benchmark", "paired-v1")
		if format, ok := r.Context().Value(pairedFormatKey{}).(string); ok {
			r.Header.Set("X-Story-Input-Format", format)
		}
	}
	return t.base.RoundTrip(r)
}

type pairSpec struct {
	ID, CaseID, FirstArm, AArm string
	Repeat                     int
}
type pairedPlan struct {
	Version                                                     string
	Seed                                                        int64
	ConfigID, PromptID, PromptSHA256, CorpusSHA256, JudgeSHA256 string
	ConfigRevision, PromptRevision                              int64
	ParallelPairs, CallDeadlineSeconds                          int
	Cases                                                       []pairedCase
	Schedule                                                    []pairSpec
	Notes                                                       []string
}
type compareOutcome struct {
	Arm, Model, ErrorCode     string
	OK                        bool
	ElapsedMS                 int64
	InputSHA256               string
	Output                    json.RawMessage `json:",omitempty"`
	Checks                    validation
	Prose                     *proseMetrics `json:",omitempty"`
	HTTPPosts, HTTPGets       int
	HTTPStatuses              []int
	InputTokens, OutputTokens int64
	StartedAt, FinishedAt     time.Time
}
type pairedResult struct {
	Spec     pairSpec
	Outcomes map[string]compareOutcome
}

func pairedHash(raw []byte) string { h := sha256.Sum256(raw); return fmt.Sprintf("%x", h) }
func savePaired(path string, v any) error {
	raw, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	temp := path + ".tmp"
	if e = os.WriteFile(temp, append(raw, '\n'), 0644); e != nil {
		return e
	}
	return os.Rename(temp, path)
}
func loadPaired(path string, v any) error {
	raw, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(raw, v)
}
func compareGenerate(ctx context.Context, arm string, model ai.StoryLLM, c pairedCase) compareOutcome {
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	trace := &turnCallTrace{}
	ctx = context.WithValue(ctx, turnTraceKey{}, trace)
	start := time.Now()
	response, err := model.Generate(ctx, ai.StoryRequest{Role: c.Role, PromptVersion: "v1", Input: c.Input, MaxTokens: 4096})
	row := compareOutcome{Arm: arm, Model: model.Identity().Model, InputSHA256: pairedHash(c.Input), StartedAt: start.UTC(), FinishedAt: time.Now().UTC(), ElapsedMS: time.Since(start).Milliseconds(), InputTokens: response.InputTokens, OutputTokens: response.OutputTokens}
	trace.mu.Lock()
	row.HTTPPosts = trace.Posts
	row.HTTPGets = trace.Gets
	row.HTTPStatuses = append([]int(nil), trace.Statuses...)
	trace.mu.Unlock()
	if err != nil {
		switch {
		case ctx.Err() != nil:
			row.ErrorCode = "deadline_or_cancelled"
		case errors.Is(err, ai.ErrInvalidOutput):
			row.ErrorCode = "invalid_output"
		default:
			row.ErrorCode = "provider_or_transport_error"
		}
		return row
	}
	var obj map[string]any
	if json.Unmarshal(response.Output, &obj) != nil || obj == nil {
		row.ErrorCode = "invalid_json_object"
		return row
	}
	row.OK = true
	row.Output = append([]byte(nil), response.Output...)
	if c.validate != nil {
		row.Checks = c.validate(obj, string(response.Output))
	}
	if c.Writer {
		row.Prose = measureProse(stringField(obj, "text"))
	}
	return row
}
func runPair(ctx context.Context, root string, m pairedModels, c pairedCase, s pairSpec) pairedResult {
	result := pairedResult{Spec: s, Outcomes: map[string]compareOutcome{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	other := "chatgpt"
	if s.FirstArm == other {
		other = "antigravity"
	}
	for _, arm := range []string{s.FirstArm, other} {
		arm := arm
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := filepath.Join(root, "outcomes", s.ID+"-"+arm+".json")
			var row compareOutcome
			if loadPaired(path, &row) == nil {
				mu.Lock()
				result.Outcomes[arm] = row
				mu.Unlock()
				return
			}
			started := path + ".started"
			if _, err := os.Stat(started); err == nil {
				row = compareOutcome{Arm: arm, Model: m.models[arm].Identity().Model, ErrorCode: "interrupted_not_retried", InputSHA256: pairedHash(c.Input)}
			} else if ctx.Err() != nil {
				row = compareOutcome{Arm: arm, ErrorCode: "not_run_cancelled"}
			} else {
				_ = savePaired(started, map[string]any{"pair": s.ID, "arm": arm, "inputSha256": pairedHash(c.Input), "started": time.Now().UTC()})
				row = compareGenerate(ctx, arm, m.models[arm], c)
			}
			_ = savePaired(path, row)
			mu.Lock()
			result.Outcomes[arm] = row
			mu.Unlock()
		}()
	}
	wg.Wait()
	_ = savePaired(filepath.Join(root, "pairs", s.ID+".json"), result)
	fmt.Printf("pair=%s chatgpt=%t/%dms antigravity=%t/%dms\n", s.ID, result.Outcomes["chatgpt"].OK, result.Outcomes["chatgpt"].ElapsedMS, result.Outcomes["antigravity"].OK, result.Outcomes["antigravity"].ElapsedMS)
	return result
}
func newPairedPlan(o pairedOptions, m pairedModels, cases []pairedCase) pairedPlan {
	raw, _ := json.Marshal(cases)
	plan := pairedPlan{Version: "paired-readable-v3-worker022", Seed: o.Seed, ConfigID: m.config.ID.String(), ConfigRevision: m.config.Revision, PromptID: m.promptsID, PromptRevision: m.promptRevision, PromptSHA256: promptFingerprint(m.prompts), CorpusSHA256: pairedHash(raw), JudgeSHA256: pairedHash([]byte(pairedJudgeContract)), ParallelPairs: 3, CallDeadlineSeconds: 180, Cases: cases, Notes: []string{
		"Primary baseline is pure antigravity-preview-05-2026, not the configured Flash/Antigravity hybrid.",
		"Each pair uses byte-identical logical input and the same frozen active prompt contract. Transport wrappers and native system-role handling differ.",
		"Both primary role arms use buffered JSON without native schema enforcement. Sampling controls are not comparable in the browser/managed agent.",
		"Three temporally matched pairs run concurrently; each pair waits for both arms before its worker advances.",
		"No failed outcome is dropped, retried for best-of selection, or converted to a model quality score. Native protocol retries/HTTP statuses are retained.",
		"Writer judgments are blinded and order-counterbalanced across both model judges; self-preference remains a limitation.",
		"No real story, Canon, production configuration or database fixtures are written.",
	}}
	rng := rand.New(rand.NewSource(o.Seed))
	for caseIndex, c := range cases {
		for rep := 1; rep <= c.Repeats; rep++ {
			s := pairSpec{ID: fmt.Sprintf("%s-r%d", c.ID, rep), CaseID: c.ID, Repeat: rep, FirstArm: "chatgpt", AArm: "chatgpt"}
			if (caseIndex+rep)%2 == 0 {
				s.FirstArm = "antigravity"
			}
			if (caseIndex+rep)%2 == 0 {
				s.AArm = "antigravity"
			}
			plan.Schedule = append(plan.Schedule, s)
		}
	}
	rng.Shuffle(len(plan.Schedule), func(i, j int) { plan.Schedule[i], plan.Schedule[j] = plan.Schedule[j], plan.Schedule[i] })
	return plan
}
func runPairedComparison(o pairedOptions) error {
	if o.Output == "" {
		return errors.New("paired comparison requires -output directory")
	}
	root, err := filepath.Abs(o.Output)
	if err != nil {
		return err
	}
	if !o.Resume {
		_ = os.MkdirAll(filepath.Dir(root), 0755)
		if err = os.Mkdir(root, 0755); err != nil {
			return errors.New("use a new output directory or explicit -paired-resume")
		}
	}
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, 50*time.Minute)
	defer cancel()
	m, err := loadPairedModels(ctx, o)
	if err != nil {
		return err
	}
	if err = waitPairedWorkers(ctx); err != nil {
		return err
	}
	savePairedBridge(ctx, root, "before")
	cases := pairedCorpus()
	plan := newPairedPlan(o, m, cases)
	if o.Resume {
		var stored pairedPlan
		if loadPaired(filepath.Join(root, "plan.json"), &stored) != nil {
			return errors.New("saved plan unavailable")
		}
		if stored.Version != plan.Version || stored.PromptSHA256 != plan.PromptSHA256 || stored.CorpusSHA256 != plan.CorpusSHA256 || stored.Seed != plan.Seed {
			return errors.New("frozen plan no longer matches; refusing resume")
		}
		plan = stored
	} else {
		if err = savePaired(filepath.Join(root, "plan.json"), plan); err != nil {
			return err
		}
	}
	_ = savePaired(filepath.Join(root, "prompt-snapshot.json"), map[string]any{"id": m.promptsID, "revision": m.promptRevision, "prompts": m.prompts.Prompts, "roleSettings": m.prompts.RoleSettings})
	fmt.Printf("paired comparison: %d pairs, prompt revision %d, seed %d\n", len(plan.Schedule), plan.PromptRevision, o.Seed)
	_ = savePaired(filepath.Join(root, "analysis-spec.json"), map[string]any{"writerWeights": map[string]float64{"continuity": .30, "agency": .25, "sceneProgress": .20, "prose": .15, "nonRepetition": .10}, "qualityTieThreshold": .20, "primaryQualityRequiresTwoValidJudges": true, "bootstrapIterations": 10000, "bootstrapUnit": "frozen scenario family; retain its repeats together", "bootstrapSeed": 20261110, "latencyPrimary": "median paired log(ChatGPT/Antigravity), both valid outputs only", "failureSensitivity": "180 seconds penalty for no valid answer within the call budget; not actual elapsed time", "scope": "fixed synthetic corpus, not a random sample of real users"})
	_ = savePaired(filepath.Join(root, "state.json"), map[string]any{"phase": "roles", "status": "running", "time": time.Now().UTC()})
	queue := make(chan pairSpec)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range queue {
				c, _ := pairedCaseByID(cases, s.CaseID)
				runPair(ctx, root, m, c, s)
			}
		}()
	}
	for _, s := range plan.Schedule {
		if ctx.Err() != nil {
			break
		}
		queue <- s
	}
	close(queue)
	wg.Wait()
	savePairedBridge(context.Background(), root, "roles")
	if ctx.Err() != nil {
		_ = savePaired(filepath.Join(root, "state.json"), map[string]any{"status": "interrupted", "time": time.Now().UTC()})
		return errors.New("comparison interrupted; checkpoints preserved")
	}
	_ = savePaired(filepath.Join(root, "state.json"), map[string]any{"phase": "judging", "status": "running", "time": time.Now().UTC()})
	if err = runPairedJudging(ctx, root, m, plan, cases); err != nil {
		return err
	}
	savePairedBridge(ctx, root, "judges")
	_ = savePaired(filepath.Join(root, "state.json"), map[string]any{"phase": "full_turns", "status": "running", "time": time.Now().UTC()})
	for index, f := range turnFixtures() {
		arms := []string{"chatgpt", "antigravity"}
		if index%2 == 1 {
			arms[0], arms[1] = arms[1], arms[0]
		}
		for _, arm := range arms {
			path := filepath.Join(root, "turns", f.ID+"-"+arm+".json")
			if _, e := os.Stat(path); e == nil {
				continue
			}
			if _, e := os.Stat(path + ".started"); e == nil {
				_ = savePaired(path, map[string]any{"arm": arm, "fixture": f.ID, "ok": false, "failure": "interrupted_not_retried"})
				continue
			}
			_ = savePaired(path+".started", map[string]string{"arm": arm, "fixture": f.ID})
			runPairedTurn(ctx, path, arm, m.models[arm], f)
		}
	}
	savePairedBridge(context.Background(), root, "after")
	if ctx.Err() != nil {
		_ = savePaired(filepath.Join(root, "state.json"), map[string]any{"phase": "full_turns", "status": "interrupted"})
		return errors.New("comparison interrupted in full turns")
	}
	_ = savePaired(filepath.Join(root, "state.json"), map[string]any{"phase": "finished", "status": "complete", "time": time.Now().UTC()})
	return nil
}
func runPairedTurn(ctx context.Context, path, arm string, model ai.StoryLLM, f turnFixture) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	start := time.Now()
	observer := &turnObserver{base: model, started: start}
	sink := &turnBenchmarkSink{started: start, provisional: true}
	canon := &turnCaptureCanon{}
	pipeline := appgen.Pipeline{LLM: observer, Planner: appgen.LLMTurnPlanner{LLM: observer}, Targets: preparedTurnFixture{Fixture: f}, Canon: canon, Sink: sink, MaxRepairs: 1, ProvisionalBeat: true}
	gid, _ := id.New()
	tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000008010"))
	_, err := pipeline.Run(ctx, gid, appgen.PlayerAction{TimelineID: tid, Text: f.Action})
	_ = savePaired(path, map[string]any{"arm": arm, "fixture": f.ID, "ok": err == nil && canon.choices == 4, "elapsedMs": time.Since(start).Milliseconds(), "firstTextMs": sink.first, "readyMs": sink.ready, "choices": canon.choices, "text": sink.text, "calls": observer.calls, "failure": func() string {
		if err != nil {
			return "pipeline_failed"
		}
		return ""
	}()})
	fmt.Printf("full_turn fixture=%s arm=%s ok=%t ms=%d\n", f.ID, arm, err == nil, time.Since(start).Milliseconds())
}
