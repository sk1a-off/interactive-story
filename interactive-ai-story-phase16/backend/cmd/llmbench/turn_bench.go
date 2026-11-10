package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	localembeddings "github.com/local/interactive-ai-story/backend/internal/adapters/embedding/local_sentence_transformers"
	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	secretfile "github.com/local/interactive-ai-story/backend/internal/adapters/secrets/file"
	storyllm "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/openai_compatible"
	storyrouter "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/role_router"
	appaiconfig "github.com/local/interactive-ai-story/backend/internal/application/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/application/contextbuilder"
	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/application/memorycontext"
	"github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationmetrics"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

type turnBenchmarkOptions struct {
	Enabled, Planner, Provisional, FastState, JournalIsolation, JSONSchema                    bool
	Provider, ConfigDSN, FixtureDSN, SecretStore, EmbeddingProvider, EmbeddingEndpoint, Cases string
	Repeats                                                                                   int
	Seed                                                                                      int64
}

func registerTurnBenchmarkFlags(f *flag.FlagSet) *turnBenchmarkOptions {
	o := &turnBenchmarkOptions{}
	f.BoolVar(&o.Enabled, "turn-bench", false, "paired synthetic full pipeline baseline; no user-story access or Canon commits")
	f.StringVar(&o.Provider, "turn-provider", "fake", "fake or configured (pinned active AI/prompt revisions)")
	f.StringVar(&o.ConfigDSN, "turn-config-database-url", "", "configured mode: read-only AI/prompt metadata database")
	f.StringVar(&o.FixtureDSN, "turn-fixture-database-url", "", "explicit isolated database for synthetic memory fixtures")
	f.StringVar(&o.SecretStore, "turn-secret-store", "../data/secrets/provider-keys.json", "read-only provider credentials; never reported")
	f.StringVar(&o.EmbeddingProvider, "turn-embedding-provider", "fake", "fake or user2; choose user2 for real memory overhead")
	f.StringVar(&o.EmbeddingEndpoint, "turn-embedding-endpoint", "http://127.0.0.1:8090", "local USER2 endpoint")
	f.StringVar(&o.Cases, "turn-cases", "", "comma-separated frozen case IDs; empty selects all")
	f.IntVar(&o.Repeats, "turn-repeats", 2, "paired repeats per case; alternate arm order")
	f.Int64Var(&o.Seed, "turn-seed", 20261109, "seed for reproducible case order")
	f.BoolVar(&o.JSONSchema, "turn-json-schema", false, "opt in to role response schemas; current runtime settings are unchanged")
	f.BoolVar(&o.Planner, "turn-planner", false, "use the existing turn planner")
	f.BoolVar(&o.Provisional, "turn-provisional", false, "measure existing draft-ready preview events")
	f.BoolVar(&o.FastState, "turn-fast-state", false, "existing Safe Hybrid journal routing")
	f.BoolVar(&o.JournalIsolation, "turn-journal-isolation", false, "existing journal-evidence isolation")
	return o
}

type turnCallTrace struct {
	mu                             sync.Mutex
	Posts, Gets, TransportFailures int
	Statuses                       []int
}
type turnTraceKey struct{}
type turnTransport struct{ base http.RoundTripper }

func (t turnTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	trace, _ := r.Context().Value(turnTraceKey{}).(*turnCallTrace)
	if trace != nil {
		trace.mu.Lock()
		if r.Method == http.MethodPost {
			trace.Posts++
		}
		if r.Method == http.MethodGet {
			trace.Gets++
		}
		trace.mu.Unlock()
	}
	resp, err := t.base.RoundTrip(r)
	if trace != nil {
		trace.mu.Lock()
		if err != nil {
			trace.TransportFailures++
		} else {
			trace.Statuses = append(trace.Statuses, resp.StatusCode)
		}
		trace.mu.Unlock()
	}
	return resp, err
}

type turnRoleCall struct {
	Streamed                               bool   `json:"streamed,omitempty"`
	StreamFallback                         bool   `json:"streamFallback,omitempty"`
	ProtocolRetries                        int    `json:"protocolRetries,omitempty"`
	CachedInputTokens                      *int64 `json:"cachedInputTokens,omitempty"`
	ReasoningEffort                        string `json:"reasoningEffort,omitempty"`
	ReasoningTokens                        *int64 `json:"reasoningTokens,omitempty"`
	ResponseFormat                         string `json:"responseFormat,omitempty"`
	SchemaFallback                         bool   `json:"schemaFallback,omitempty"`
	Sequence                               int    `json:"sequence"`
	Role                                   string `json:"role"`
	Model                                  string `json:"model"`
	StartMS, EndMS, ElapsedMS              float64
	InputTokens, OutputTokens              int64
	InputBytes                             int
	Repair                                 bool
	Failed                                 bool
	EscalatedFrom                          string `json:"escalatedFrom,omitempty"`
	HTTPPosts, HTTPGets, TransportFailures int
	HTTPStatuses                           []int
	MemoryDocuments                        int `json:"memoryDocuments"`
}
type turnObserver struct {
	base     ai.StoryLLM
	started  time.Time
	mu       sync.Mutex
	sequence int
	calls    []turnRoleCall
	progress bool
}

func (o *turnObserver) Identity() ai.ProviderIdentity { return o.base.Identity() }
func (o *turnObserver) Generate(ctx context.Context, r ai.StoryRequest) (ai.StoryResponse, error) {
	o.mu.Lock()
	o.sequence++
	seq := o.sequence
	o.mu.Unlock()
	trace := &turnCallTrace{}
	start := time.Now()
	response, err := o.base.Generate(context.WithValue(ctx, turnTraceKey{}, trace), r)
	end := time.Now()
	model := response.Provider.Model
	if model == "" {
		model = o.base.Identity().Model
	}
	call := turnRoleCall{Sequence: seq, Role: r.Role, Model: model, StartMS: float64(start.Sub(o.started)) / float64(time.Millisecond), EndMS: float64(end.Sub(o.started)) / float64(time.Millisecond), ElapsedMS: float64(end.Sub(start)) / float64(time.Millisecond), InputTokens: response.InputTokens, OutputTokens: response.OutputTokens, InputBytes: len(r.Input), Repair: r.Repair || r.Role == "structured_repair", Failed: err != nil, EscalatedFrom: response.EscalatedFrom, ResponseFormat: response.ResponseFormat, SchemaFallback: response.SchemaFallback, Streamed: response.Streamed, StreamFallback: response.StreamFallback, ProtocolRetries: response.ProtocolRetries, ReasoningEffort: response.ReasoningEffort, ReasoningTokens: response.ReasoningTokens, CachedInputTokens: response.CachedInputTokens, HTTPPosts: trace.Posts, HTTPGets: trace.Gets, TransportFailures: trace.TransportFailures, HTTPStatuses: trace.Statuses}
	var input struct {
		Memory struct {
			Evidence []json.RawMessage `json:"evidence"`
		} `json:"retrievedMemory"`
	}
	_ = json.Unmarshal(r.Input, &input)
	call.MemoryDocuments = len(input.Memory.Evidence)
	o.mu.Lock()
	o.calls = append(o.calls, call)
	o.mu.Unlock()
	if o.progress {
		fmt.Printf("role=%s model=%s elapsed_ms=%.0f http_posts=%d http_gets=%d ok=%t\n", call.Role, call.Model, call.ElapsedMS, call.HTTPPosts, call.HTTPGets, !call.Failed)
	}
	return response, err
}

type turnTimings struct {
	Count    int     `json:"count"`
	MedianMS float64 `json:"medianMs"`
	P95MS    float64 `json:"p95Ms"`
}
type turnArmSummary struct {
	Memory                  bool `json:"memory"`
	Runs, Succeeded, Failed int
	FirstText, Ready, Total turnTimings
	Roles                   []turnRoleSummary
	MemorySkipped           int `json:"memorySkipped"`
}
type turnRoleSummary struct {
	Role, Model                                   string
	Calls, Repairs, Failures, HTTPPosts, HTTPGets int
	InputTokens, OutputTokens                     int64
	CallLatency                                   turnTimings
}
type turnBenchmarkRun struct {
	Case                       string   `json:"case"`
	Repeat                     int      `json:"repeat"`
	Pair                       string   `json:"pair"`
	Memory                     bool     `json:"memory"`
	Order                      int      `json:"order"`
	Succeeded                  bool     `json:"succeeded"`
	Failure                    string   `json:"failure,omitempty"`
	ElapsedMS                  float64  `json:"elapsedMs"`
	DraftReadyMS               *float64 `json:"draftReadyMs,omitempty"`
	FirstAvailableTextMS       *float64 `json:"firstAvailableTextMs,omitempty"`
	ReadyMS                    *float64 `json:"readyMs,omitempty"`
	Text                       string   `json:"text,omitempty"`
	Words, Paragraphs, Choices int
	Calls                      []turnRoleCall            `json:"calls"`
	Stages                     generationmetrics.Metrics `json:"stages"`
	MemoryDocuments            int                       `json:"memoryDocuments"`
}
type turnBenchmarkReport struct {
	SchemaSHA256                                      string `json:"schemaSha256,omitempty"`
	JSONSchema                                        bool   `json:"jsonSchema"`
	Mode, Status                                      string
	StartedAt, FinishedAt                             time.Time
	Provider                                          string
	ConfigID                                          id.ID
	ConfigRevision                                    int64
	PromptID                                          id.ID
	PromptRevision                                    int64
	PromptSHA256, FixtureSHA256                       string
	PrimaryModel                                      string
	SafeHybrid                                        bool
	Planner, Provisional, FastState, JournalIsolation bool
	Embedding                                         ai.ProviderIdentity
	Repeats                                           int
	Seed                                              int64
	FixtureOwner                                      id.ID
	CleanupVerified                                   bool
	Runs                                              []turnBenchmarkRun
	Summary                                           []turnArmSummary
	Notes                                             []string
	Failure                                           string          `json:"failure,omitempty"`
	Paired                                            []turnPairDelta `json:"paired"`
}
type turnPairDelta struct {
	Pair                          string `json:"pair"`
	FirstTextMS, ReadyMS, TotalMS float64
	InputTokens, OutputTokens     int64
	Words                         int
}

func pairedTurnDeltas(runs []turnBenchmarkRun) []turnPairDelta {
	byPair := map[string][]turnBenchmarkRun{}
	for _, r := range runs {
		byPair[r.Pair] = append(byPair[r.Pair], r)
	}
	var out []turnPairDelta
	for key, rows := range byPair {
		if len(rows) != 2 {
			continue
		}
		off, on := rows[0], rows[1]
		if off.Memory {
			off, on = on, off
		}
		if off.Memory || !on.Memory || !off.Succeeded || !on.Succeeded || on.MemoryDocuments == 0 || off.FirstAvailableTextMS == nil || on.FirstAvailableTextMS == nil || off.ReadyMS == nil || on.ReadyMS == nil {
			continue
		}
		d := turnPairDelta{Pair: key, FirstTextMS: *on.FirstAvailableTextMS - *off.FirstAvailableTextMS, ReadyMS: *on.ReadyMS - *off.ReadyMS, TotalMS: on.ElapsedMS - off.ElapsedMS, Words: on.Words - off.Words}
		for _, c := range on.Calls {
			d.InputTokens += c.InputTokens
			d.OutputTokens += c.OutputTokens
		}
		for _, c := range off.Calls {
			d.InputTokens -= c.InputTokens
			d.OutputTokens -= c.OutputTokens
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pair < out[j].Pair })
	return out
}

func turnDistribution(samples []float64) turnTimings {
	if len(samples) == 0 {
		return turnTimings{}
	}
	s := append([]float64(nil), samples...)
	sort.Float64s(s)
	med := s[len(s)/2]
	if len(s)%2 == 0 {
		med = (s[len(s)/2-1] + med) / 2
	}
	return turnTimings{Count: len(s), MedianMS: med, P95MS: s[int(math.Ceil(.95*float64(len(s))))-1]}
}
func summarizeTurnRuns(runs []turnBenchmarkRun) []turnArmSummary {
	var out []turnArmSummary
	for _, enabled := range []bool{false, true} {
		s := turnArmSummary{Memory: enabled}
		var first, ready, total []float64
		byRole := map[string]*turnRoleSummary{}
		times := map[string][]float64{}
		for _, run := range runs {
			if run.Memory != enabled {
				continue
			}
			s.Runs++
			if run.Memory && run.MemoryDocuments == 0 {
				s.MemorySkipped++
			}
			if run.Succeeded {
				s.Succeeded++
				total = append(total, run.ElapsedMS)
				if run.FirstAvailableTextMS != nil {
					first = append(first, *run.FirstAvailableTextMS)
				}
				if run.ReadyMS != nil {
					ready = append(ready, *run.ReadyMS)
				}
			} else {
				s.Failed++
			}
			for _, call := range run.Calls {
				key := call.Role + "/" + call.Model
				r := byRole[key]
				if r == nil {
					r = &turnRoleSummary{Role: call.Role, Model: call.Model}
					byRole[key] = r
				}
				r.Calls++
				if call.Repair {
					r.Repairs++
				}
				if call.Failed {
					r.Failures++
				}
				r.InputTokens += call.InputTokens
				r.OutputTokens += call.OutputTokens
				r.HTTPPosts += call.HTTPPosts
				r.HTTPGets += call.HTTPGets
				times[key] = append(times[key], call.ElapsedMS)
			}
		}
		for key, r := range byRole {
			r.CallLatency = turnDistribution(times[key])
			s.Roles = append(s.Roles, *r)
		}
		sort.Slice(s.Roles, func(i, j int) bool {
			if s.Roles[i].Role == s.Roles[j].Role {
				return s.Roles[i].Model < s.Roles[j].Model
			}
			return s.Roles[i].Role < s.Roles[j].Role
		})
		s.FirstText, s.Ready, s.Total = turnDistribution(first), turnDistribution(ready), turnDistribution(total)
		out = append(out, s)
	}
	return out
}
func turnBenchmarkConfigured(ctx context.Context, o turnBenchmarkOptions) (aiconfig.Revision, promptset.Revision, []string, error) {
	c, err := pgxpool.ParseConfig(o.ConfigDSN)
	if err != nil {
		return aiconfig.Revision{}, promptset.Revision{}, nil, errors.New("configuration database invalid")
	}
	c.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	pool, err := pgxpool.NewWithConfig(ctx, c)
	if err != nil {
		return aiconfig.Revision{}, promptset.Revision{}, nil, errors.New("configuration database unavailable")
	}
	defer pool.Close()
	rev, err := pg.NewAIConfigRepository(pool).Active(ctx)
	if err != nil {
		return rev, promptset.Revision{}, nil, errors.New("active AI revision unavailable")
	}
	prompts, err := pg.NewPromptSetRepository(pool).Active(ctx)
	if err != nil {
		return rev, prompts, nil, errors.New("active prompt revision unavailable")
	}
	store, err := secretfile.NewReadOnly(o.SecretStore)
	if err != nil {
		return rev, prompts, nil, errors.New("credential store unavailable")
	}
	keys := appaiconfig.CredentialKeys(ctx, store, rev.ID)
	if rev.StoryLLM.Provider == aiconfig.ProviderGoogleGemini && len(keys) == 0 {
		return rev, prompts, nil, errors.New("configured model credentials unavailable")
	}
	return rev, prompts, keys, nil
}
func turnBenchmarkModel(o turnBenchmarkOptions, rev aiconfig.Revision, prompt promptset.Revision, keys []string, fixture turnFixture) (ai.StoryLLM, error) {
	if o.Provider == "fake" {
		return fakeTurnModel(fixture), nil
	}
	var settings aiconfig.PublicSettings
	if json.Unmarshal(rev.Settings, &settings) != nil {
		return nil, errors.New("invalid AI settings")
	}
	client := func(model, profile string, timeout time.Duration) (ai.StoryLLM, error) {
		return storyllm.New(storyllm.Config{BaseURL: settings.StoryLLMEndpoint, APIKeys: keys, Provider: rev.StoryLLM.Provider, Model: model, Profile: profile, Timeout: timeout, PromptSet: &prompt, JSONSchema: o.JSONSchema}, &http.Client{Timeout: timeout, Transport: turnTransport{base: http.DefaultTransport}})
	}
	if settings.StoryLLMSafeHybrid {
		fast, err := client(aiconfig.Gemini35FlashLiteModel, "safe-hybrid-fast", 20*time.Minute)
		if err != nil {
			return nil, err
		}
		quality, err := client(aiconfig.AntigravityModel, "safe-hybrid-quality", 12*time.Minute)
		if err != nil {
			return nil, err
		}
		return storyrouter.NewSafeHybridWithFastState(fast, quality, o.FastState)
	}
	return client(rev.StoryLLM.Model, rev.StoryLLM.Profile, 120*time.Second)
}

func runTurnBenchmark(o turnBenchmarkOptions, output string) (runErr error) {
	r := turnBenchmarkReport{SchemaSHA256: storyllm.ResponseSchemaFingerprint(), JSONSchema: o.JSONSchema, Mode: "paired-full-turn-v1", Status: "failed", StartedAt: time.Now().UTC(), Provider: o.Provider, Planner: o.Planner, Provisional: o.Provisional, FastState: o.FastState, JournalIsolation: o.JournalIsolation, Repeats: o.Repeats, Seed: o.Seed, Notes: []string{
		"Frozen synthetic targets only; configuration DB is read-only, credentials and user-story content are never reported.",
		"Production Pipeline and Safe Hybrid routing are used. Memory-off/on pairs share the same initial target and action; generated prose can differ.",
		"First available text is a draft-ready event when enabled, otherwise completion; client network/render time, durable queue, and real SQL Canon commit are excluded.",
		"Per-role wall times overlap. HTTP POST/GET counts distinguish provider polling from role calls; extra HTTP requests are not automatically retries.",
		"Reported usage belongs to logical response telemetry and can omit rejected/escalated calls. Zero tokens do not prove zero provider consumption.",
		"Latency summaries use succeeded turns; failed elapsed times remain in per-run records. This does not validate literary quality or measure durable telemetry persistence.",
	}}
	save := func() error {
		r.Summary = summarizeTurnRuns(r.Runs)
		r.Paired = pairedTurnDeltas(r.Runs)
		raw, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(output, append(raw, '\n'), 0600)
	}
	defer func() {
		r.FinishedAt = time.Now().UTC()
		if output != "" {
			if err := save(); err != nil {
				runErr = errors.New("turn report write failed")
			}
		}
	}()
	fail := func(why string) error {
		r.Status = "failed"
		r.Failure = why
		return errors.New("turn benchmark failed: " + why)
	}
	if output == "" || o.FixtureDSN == "" || o.Repeats < 1 || o.Repeats > 100 {
		return fail("explicit_fixture_database_output_and_valid_repeats_required")
	}
	if o.Provider != "fake" && o.Provider != "configured" {
		return fail("unknown_provider")
	}
	if o.Provider == "configured" && o.ConfigDSN == "" {
		return fail("read_only_configuration_database_required")
	}
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(rootCtx, 90*time.Minute)
	defer cancel()
	var rev aiconfig.Revision
	var prompts promptset.Revision
	var keys []string
	var err error
	if o.Provider == "configured" {
		rev, prompts, keys, err = turnBenchmarkConfigured(ctx, o)
		if err != nil {
			return fail(err.Error())
		}
		r.ConfigID, r.ConfigRevision, r.PromptID, r.PromptRevision, r.PrimaryModel = rev.ID, rev.Revision, prompts.ID, prompts.Revision, rev.StoryLLM.Model
		var s aiconfig.PublicSettings
		_ = json.Unmarshal(rev.Settings, &s)
		r.SafeHybrid = s.StoryLLMSafeHybrid
	}
	pr, _ := json.Marshal(prompts)
	hash := sha256.Sum256(pr)
	r.PromptSHA256 = hex.EncodeToString(hash[:])
	fixtures := turnFixtures()
	fr, _ := json.Marshal(fixtures)
	hash = sha256.Sum256(fr)
	r.FixtureSHA256 = hex.EncodeToString(hash[:])
	if o.Cases != "" {
		var selected []turnFixture
		for _, f := range fixtures {
			for _, name := range strings.Split(o.Cases, ",") {
				if f.ID == strings.TrimSpace(name) {
					selected = append(selected, f)
					break
				}
			}
		}
		fixtures = selected
	}
	if len(fixtures) == 0 {
		return fail("no_fixture_cases")
	}
	var embeddings ai.EmbeddingProvider
	switch o.EmbeddingProvider {
	case "fake":
		embeddings = fakeai.NewEmbeddingProvider(384)
		r.Notes = append(r.Notes, "Fake embeddings measure wiring only, not semantic memory quality.")
	case "user2":
		healthCtx, healthCancel := context.WithTimeout(ctx, 3*time.Second)
		req, healthErr := http.NewRequestWithContext(healthCtx, http.MethodGet, strings.TrimRight(o.EmbeddingEndpoint, "/")+"/health", nil)
		if healthErr != nil {
			healthCancel()
			return fail("embedding_health_configuration_invalid")
		}
		resp, healthErr := http.DefaultClient.Do(req)
		if healthErr != nil {
			healthCancel()
			return fail("embedding_service_unavailable")
		}
		var health struct {
			Model      string `json:"model"`
			Dimensions int    `json:"dimensions"`
		}
		healthErr = json.NewDecoder(resp.Body).Decode(&health)
		_ = resp.Body.Close()
		healthCancel()
		if healthErr != nil || resp.StatusCode != 200 || health.Model != "deepvk/USER2-small" || health.Dimensions != 384 {
			return fail("embedding_service_identity_mismatch")
		}
		embeddings, err = localembeddings.New(o.EmbeddingEndpoint, "deepvk/USER2-small", nil)
	default:
		return fail("unknown_embedding_provider")
	}
	if err != nil {
		return fail("embedding_configuration_failed")
	}
	r.Embedding = embeddings.Identity()
	pool, err := pgxpool.New(ctx, o.FixtureDSN)
	if err != nil {
		return fail("fixture_database_unavailable")
	}
	defer pool.Close()
	owner, err := id.New()
	if err != nil {
		return fail("fixture_identity_failed")
	}
	r.FixtureOwner = owner
	prepared, err := prepareTurnFixtures(ctx, pool, owner, fixtures, embeddings)
	if err != nil {
		return fail("fixture_preparation_failed")
	}
	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanCancel()
		if err := cleanupTurnFixtures(cleanCtx, pool, owner); err != nil {
			r.CleanupVerified = false
			r.Status = "failed"
			r.Failure = "fixture_cleanup_failed"
			runErr = errors.New("turn fixture cleanup failed")
		} else {
			r.CleanupVerified = true
		}
	}()
	r.Status = "running"
	if err = save(); err != nil {
		return fail("report_checkpoint_failed")
	}
	r.Status = "running"
	rng := rand.New(rand.NewSource(o.Seed))
	for repeat := 0; repeat < o.Repeats; repeat++ {
		for _, i := range rng.Perm(len(prepared)) {
			fixture := prepared[i]
			arms := []bool{false, true}
			if (repeat+i)%2 == 1 {
				arms[0], arms[1] = true, false
			}
			for order, enabled := range arms {
				model, err := turnBenchmarkModel(o, rev, prompts, keys, fixture.Fixture)
				if err != nil {
					return fail("model_configuration_failed")
				}
				started := time.Now()
				observer := &turnObserver{base: model, started: started, progress: o.Provider == "configured"}
				sink := &turnBenchmarkSink{started: started, provisional: o.Provisional}
				canon := &turnCaptureCanon{}
				var stages generationmetrics.Metrics
				pipeline := appgen.Pipeline{LLM: observer, Canon: canon, Targets: fixture, Sink: sink, MaxRepairs: 1, ProvisionalBeat: o.Provisional, JournalEvidenceIsolationV1: o.JournalIsolation, Metrics: func(m generationmetrics.Metrics) { m.ErrorText = ""; stages = m }}
				if o.Planner {
					pipeline.Planner = appgen.LLMTurnPlanner{LLM: observer}
				}
				if enabled {
					pipeline.Memory = &memorycontext.Service{TopK: 8, MemoryTokenBudget: 1024, Builder: contextbuilder.Builder{Embeddings: embeddings, Memory: pg.NewHybridMemoryRetriever(pool), Visibility: pg.NewMemoryVisibilityResolver(pool), Profile: memory.EmbeddingProfile{Provider: r.Embedding.Provider, Model: r.Embedding.Model, Revision: 1}, RetrievalTimeout: time.Second}}
				}
				gid, _ := id.New()
				_, err = pipeline.Run(ctx, gid, appgen.PlayerAction{StoryID: fixture.StoryID, TimelineID: fixture.TimelineID, ExpectedHead: 12, Text: fixture.Fixture.Action})
				row := turnBenchmarkRun{Case: fixture.Fixture.ID, Repeat: repeat + 1, Pair: fmt.Sprintf("%s/%d", fixture.Fixture.ID, repeat+1), Memory: enabled, Order: order + 1, Succeeded: err == nil, ElapsedMS: float64(time.Since(started)) / float64(time.Millisecond), DraftReadyMS: sink.draft, FirstAvailableTextMS: sink.first, ReadyMS: sink.ready, Text: sink.text, Calls: observer.calls, Stages: stages}
				if err != nil {
					row.Failure = "pipeline_failed"
				}
				row.Words = len(strings.Fields(row.Text))
				for _, call := range row.Calls {
					if call.MemoryDocuments > row.MemoryDocuments {
						row.MemoryDocuments = call.MemoryDocuments
					}
				}
				row.Paragraphs = len(splitParagraphs(row.Text))
				row.Choices = canon.choices
				sort.Slice(row.Calls, func(i, j int) bool { return row.Calls[i].Sequence < row.Calls[j].Sequence })
				r.Runs = append(r.Runs, row)
				if err = save(); err != nil {
					return fail("report_checkpoint_failed")
				}
				fmt.Printf("case=%s repeat=%d memory=%t completed=%t elapsed_ms=%.1f calls=%d\n", row.Case, row.Repeat, row.Memory, row.Succeeded, row.ElapsedMS, len(row.Calls))
				if ctx.Err() != nil {
					return fail("run_cancelled_or_timed_out")
				}
				if !row.Succeeded {
					for _, call := range row.Calls {
						for _, status := range call.HTTPStatuses {
							if status == 401 || status == 403 || status == 404 {
								return fail("provider_rejected_configured_request")
							}
						}
					}
				}
			}
		}
	}
	r.Status = "measured"
	for _, row := range r.Runs {
		if !row.Succeeded || (row.Memory && row.MemoryDocuments == 0) {
			return fail("incomplete_or_failed_turns")
		}
	}
	if o.Provider == "fake" {
		r.Status = "smoke_only"
	}
	return nil
}
