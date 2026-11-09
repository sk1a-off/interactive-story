package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	localembeddings "github.com/local/interactive-ai-story/backend/internal/adapters/embedding/local_sentence_transformers"
	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/application/contextbuilder"
	"github.com/local/interactive-ai-story/backend/internal/application/memorycontext"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/memory"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

//go:embed fixtures/memory-retrieval-v1.json
var memoryFixtures embed.FS

type memoryBenchmarkOptions struct {
	Enabled                         bool
	DSN, Provider, Endpoint, Corpus string
	Repeats                         int
	Seed                            int64
}

func registerMemoryBenchmarkFlags(f *flag.FlagSet) *memoryBenchmarkOptions {
	o := &memoryBenchmarkOptions{}
	f.BoolVar(&o.Enabled, "memory-bench", false, "measure frozen memory retrieval, without text LLM calls")
	f.StringVar(&o.DSN, "memory-database-url", "", "explicit isolated migrated PostgreSQL database for rolled-back fixtures; no default to the story database")
	f.StringVar(&o.Provider, "memory-provider", "user2", "user2 for semantic evaluation; fake for mechanical smoke only")
	f.StringVar(&o.Endpoint, "memory-embedding-endpoint", "http://127.0.0.1:8090", "local USER2 endpoint")
	f.StringVar(&o.Corpus, "memory-corpus", "", "optional frozen memory corpus JSON; built-in v1 by default")
	f.IntVar(&o.Repeats, "memory-repeats", 20, "search timing repeats per unique question and k; does not increase semantic sample size")
	f.Int64Var(&o.Seed, "memory-seed", 20261109, "reproducible search order")
	return o
}

type memoryBenchDocument struct {
	ID, Timeline, Owner, Class, Kind, Text, Profile string
	From                                            int64
	To                                              *int64
}
type memoryBenchScope struct {
	ID, Timeline, Owner            string
	At                             int64
	Allowed                        []string
	Lag, ForeignStory, ExpectError bool
}
type memoryBenchQuestion struct {
	ID, Scope, Text string
	Relevant        []string
}
type memoryBenchCorpus struct {
	Revision  int
	Documents []memoryBenchDocument
	Scopes    []memoryBenchScope
	Questions []memoryBenchQuestion
}
type memoryBenchDistribution struct {
	Count    int     `json:"count"`
	MedianMS float64 `json:"medianMs"`
	P95MS    float64 `json:"p95Ms"`
	MaxMS    float64 `json:"maxMs"`
}
type memoryBenchRow struct {
	Question   string   `json:"question"`
	K          int      `json:"k"`
	Relevant   int      `json:"relevant"`
	Recall     *float64 `json:"recall,omitempty"`
	Returned   []string `json:"returned"`
	Unexpected []string `json:"unexpected,omitempty"`
	Failure    string   `json:"failure,omitempty"`
}
type memoryBenchRecall struct {
	K           int     `json:"k"`
	Questions   int     `json:"questions"`
	MacroRecall float64 `json:"macroRecall"`
}
type memoryBenchKLatency struct {
	K      int                     `json:"k"`
	Search memoryBenchDistribution `json:"search"`
}
type memoryBenchBudgetResult struct {
	Tokens                int              `json:"tokens"`
	Questions             int              `json:"questions"`
	MacroRecall           float64          `json:"macroRecall"`
	MeanRetainedDocuments float64          `json:"meanRetainedDocuments"`
	Rows                  []memoryBenchRow `json:"rows"`
}
type memoryBenchReport struct {
	Mode                string                    `json:"mode"`
	Status              string                    `json:"status"`
	SemanticEvaluation  bool                      `json:"semanticEvaluation"`
	Provider            ai.ProviderIdentity       `json:"provider"`
	StartedAt           time.Time                 `json:"startedAt"`
	FinishedAt          time.Time                 `json:"finishedAt"`
	CorpusSHA256        string                    `json:"corpusSha256"`
	CorpusRevision      int                       `json:"corpusRevision"`
	Documents           int                       `json:"documents"`
	UniqueQuestions     int                       `json:"uniqueQuestions"`
	Repeats             int                       `json:"repeats"`
	Seed                int64                     `json:"seed"`
	PostgresVersion     string                    `json:"postgresVersion,omitempty"`
	PgvectorVersion     string                    `json:"pgvectorVersion,omitempty"`
	GoVersion           string                    `json:"goVersion"`
	OS                  string                    `json:"os"`
	Arch                string                    `json:"arch"`
	VisibleCPUs         int                       `json:"visibleCpus"`
	EmbeddingDevice     string                    `json:"embeddingDevice,omitempty"`
	DocumentEmbedding   memoryBenchDistribution   `json:"documentEmbeddingBatches"`
	QueryEmbedding      memoryBenchDistribution   `json:"queryEmbedding"`
	Search              memoryBenchDistribution   `json:"search"`
	SearchByK           []memoryBenchKLatency     `json:"searchByK"`
	ContextBudgets      []memoryBenchBudgetResult `json:"contextBudgets"`
	RejectedScope       memoryBenchDistribution   `json:"rejectedScopeSearch"`
	WarmupSearches      int                       `json:"warmupSearches"`
	Recall              []memoryBenchRecall       `json:"recall"`
	IsolationViolations int                       `json:"isolationViolations"`
	SearchFailures      int                       `json:"searchFailures"`
	UnstableResults     int                       `json:"unstableResults"`
	Rows                []memoryBenchRow          `json:"rows"`
	Notes               []string                  `json:"notes"`
	Failure             string                    `json:"failure,omitempty"`
}

func loadMemoryCorpus(raw []byte) (memoryBenchCorpus, error) {
	var c memoryBenchCorpus
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	if c.Revision < 1 || len(c.Documents) == 0 || len(c.Questions) == 0 {
		return c, errors.New("empty memory corpus")
	}
	docs := map[string]bool{}
	scopes := map[string]memoryBenchScope{}
	questions := map[string]bool{}
	for _, d := range c.Documents {
		if d.ID == "" || d.Text == "" || docs[d.ID] {
			return c, errors.New("invalid/duplicate memory document")
		}
		if _, ok := memoryBenchHeads[d.Timeline]; !ok || d.From < 0 || (d.To != nil && *d.To < d.From) {
			return c, errors.New("invalid document scope/interval")
		}
		if d.Class != "" && d.Class != "historical_beat" && d.Class != "branch_state" {
			return c, errors.New("invalid document class")
		}
		if d.Profile != "" && d.Profile != "other-model" && d.Profile != "other-revision" {
			return c, errors.New("invalid embedding profile fixture")
		}
		docs[d.ID] = true
	}
	for _, s := range c.Scopes {
		if s.ID == "" {
			return c, errors.New("missing scope")
		}
		if _, ok := scopes[s.ID]; ok {
			return c, errors.New("duplicate scope")
		}
		for _, key := range s.Allowed {
			if !docs[key] {
				return c, errors.New("unknown allowed document")
			}
		}
		scopes[s.ID] = s
	}
	for _, q := range c.Questions {
		s, ok := scopes[q.Scope]
		if q.ID == "" || q.Text == "" || questions[q.ID] || !ok {
			return c, errors.New("invalid/duplicate question")
		}
		questions[q.ID] = true
		seen := map[string]bool{}
		for _, key := range q.Relevant {
			if !containsMemoryKey(s.Allowed, key) || seen[key] {
				return c, errors.New("relevance must be unique and inside annotated allowed scope")
			}
			seen[key] = true
		}
		if s.ExpectError && len(q.Relevant) > 0 {
			return c, errors.New("invalid scope cannot have relevant documents")
		}
	}
	return c, nil
}
func containsMemoryKey(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}
func memoryDistribution(samples []float64) memoryBenchDistribution {
	if len(samples) == 0 {
		return memoryBenchDistribution{}
	}
	x := append([]float64(nil), samples...)
	sort.Float64s(x)
	median := x[len(x)/2]
	if len(x)%2 == 0 {
		median = (x[len(x)/2-1] + median) / 2
	}
	return memoryBenchDistribution{Count: len(x), MedianMS: median, P95MS: x[int(math.Ceil(.95*float64(len(x))))-1], MaxMS: x[len(x)-1]}
}
func evaluateMemoryResult(q memoryBenchQuestion, s memoryBenchScope, k int, found []memory.Memory, err error) memoryBenchRow {
	r := memoryBenchRow{Question: q.ID, K: k, Relevant: len(q.Relevant), Returned: []string{}}
	if len(q.Relevant) > 0 {
		zero := float64(0)
		r.Recall = &zero
	}
	if s.ExpectError {
		if !errors.Is(err, domain.ErrInvalidVisibility) || len(found) != 0 {
			r.Failure = "expected_visibility_rejection"
		}
		for _, m := range found {
			r.Unexpected = append(r.Unexpected, m.SourceKey)
		}
		return r
	}
	if err != nil {
		r.Failure = "search_failed"
		return r
	}
	seen := map[string]bool{}
	hits := 0
	for index, m := range found {
		r.Returned = append(r.Returned, m.SourceKey)
		if seen[m.SourceKey] || !containsMemoryKey(s.Allowed, m.SourceKey) {
			r.Unexpected = append(r.Unexpected, m.SourceKey)
		}
		if index < k && !seen[m.SourceKey] && containsMemoryKey(q.Relevant, m.SourceKey) {
			hits++
		}
		seen[m.SourceKey] = true
	}
	if len(found) > k {
		r.Failure = "result_exceeds_k"
	}
	if len(q.Relevant) > 0 {
		recall := float64(hits) / float64(len(q.Relevant))
		r.Recall = &recall
	}
	return r
}
func memoryBenchID(key string) id.ID {
	h := sha256.Sum256([]byte("story-memory-benchmark-v1/" + key))
	h[6] = (h[6] & 15) | 64
	h[8] = (h[8] & 63) | 128
	x := hex.EncodeToString(h[:16])
	return id.ID(x[:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:])
}
func validMemoryBenchVectors(vectors [][]float32, count int) bool {
	if len(vectors) != count {
		return false
	}
	for _, v := range vectors {
		if len(v) != 384 {
			return false
		}
		norm := float64(0)
		for _, x := range v {
			if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
				return false
			}
			norm += float64(x) * float64(x)
		}
		if norm == 0 {
			return false
		}
	}
	return true
}

func runMemoryBenchmark(o memoryBenchmarkOptions, output string) (runErr error) {
	r := memoryBenchReport{Mode: "memory-retrieval-v1", Status: "failed", SemanticEvaluation: o.Provider == "user2", StartedAt: time.Now().UTC(), Repeats: o.Repeats, Seed: o.Seed,
		Notes: []string{"Fixed, manually annotated synthetic Russian corpus; not a sample of real stories.", "Repeated searches measure timing/reproducibility; recall uses one result per unique question and k.", "Search time includes trusted lineage and ranking; query embedding is reported separately. No text LLM or generation latency is measured.", "Warm-cache, single-client exact filtered search; no large-corpus or concurrent-load claim."}}
	r.GoVersion, r.OS, r.Arch, r.VisibleCPUs = runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU()
	r.Notes = append(r.Notes, "Context-budget replay uses cached query vectors and the real narrative supplement service at top-k=8; private-character questions are excluded. Recent-beat deduplication is not simulated.")
	defer func() {
		r.FinishedAt = time.Now().UTC()
		if output != "" {
			raw, err := json.MarshalIndent(r, "", "  ")
			if err == nil {
				err = os.WriteFile(output, append(raw, '\n'), 0600)
			}
			if err != nil {
				runErr = errors.New("memory report could not be saved")
			}
		}
	}()
	fail := func(reason string) error { r.Failure = reason; return errors.New("memory benchmark failed: " + reason) }
	if output == "" || o.DSN == "" || o.Repeats < 1 || o.Repeats > 1000 {
		return fail("explicit_isolated_database_output_and_valid_repeats_required")
	}
	raw, err := memoryFixtures.ReadFile("fixtures/memory-retrieval-v1.json")
	if o.Corpus != "" {
		raw, err = os.ReadFile(o.Corpus)
	}
	if err != nil {
		return fail("corpus_read_failed")
	}
	c, err := loadMemoryCorpus(raw)
	if err != nil {
		return fail("corpus_invalid")
	}
	hash := sha256.Sum256(raw)
	r.CorpusSHA256 = hex.EncodeToString(hash[:])
	r.CorpusRevision = c.Revision
	r.Documents = len(c.Documents)
	r.UniqueQuestions = len(c.Questions)
	var embeddings ai.EmbeddingProvider
	switch o.Provider {
	case "fake":
		embeddings = fakeai.NewEmbeddingProvider(384)
		r.Notes = append(r.Notes, "FAKE length-based embeddings: recall values are mechanical diagnostics, NOT semantic quality evidence.")
	case "user2":
		embeddings, err = localembeddings.New(o.Endpoint, "deepvk/USER2-small", nil)
	default:
		return fail("unknown_embedding_provider")
	}
	if err != nil {
		return fail("embedding_configuration_invalid")
	}
	r.Provider = embeddings.Identity()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if o.Provider == "user2" {
		healthCtx, healthCancel := context.WithTimeout(ctx, 3*time.Second)
		req, reqErr := http.NewRequestWithContext(healthCtx, http.MethodGet, strings.TrimRight(o.Endpoint, "/")+"/health", nil)
		if reqErr != nil {
			healthCancel()
			return fail("embedding_health_configuration_invalid")
		}
		resp, healthErr := http.DefaultClient.Do(req)
		if healthErr != nil {
			healthCancel()
			return fail("embedding_health_unavailable")
		}
		var health struct {
			Model      string `json:"model"`
			Dimensions int    `json:"dimensions"`
			Device     string `json:"device"`
		}
		healthErr = json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&health)
		_ = resp.Body.Close()
		healthCancel()
		if healthErr != nil || resp.StatusCode != 200 || health.Model != "deepvk/USER2-small" || health.Dimensions != 384 {
			return fail("embedding_health_identity_mismatch")
		}
		r.EmbeddingDevice = health.Device
	}
	var documentVectors [][]float32
	var docTimes, queryTimes []float64
	for offset := 0; offset < len(c.Documents); offset += 64 {
		end := min(offset+64, len(c.Documents))
		texts := make([]string, end-offset)
		for i, d := range c.Documents[offset:end] {
			texts[i] = d.Text
		}
		start := time.Now()
		response, err := embeddings.EmbedDocuments(ctx, ai.EmbeddingRequest{Texts: texts})
		docTimes = append(docTimes, float64(time.Since(start))/float64(time.Millisecond))
		if err != nil || !validMemoryBenchVectors(response.Vectors, len(texts)) {
			return fail("document_embedding_unavailable_or_invalid")
		}
		documentVectors = append(documentVectors, response.Vectors...)
	}
	r.DocumentEmbedding = memoryDistribution(docTimes)
	queryVectors := make([][]float32, len(c.Questions))
	for i, q := range c.Questions {
		start := time.Now()
		response, err := embeddings.EmbedQueries(ctx, ai.EmbeddingRequest{Texts: []string{q.Text}})
		queryTimes = append(queryTimes, float64(time.Since(start))/float64(time.Millisecond))
		if err != nil || !validMemoryBenchVectors(response.Vectors, 1) {
			return fail("query_embedding_unavailable_or_invalid")
		}
		queryVectors[i] = response.Vectors[0]
	}
	r.QueryEmbedding = memoryDistribution(queryTimes)
	// Model calls above finish before the fixture transaction begins.
	pool, err := bootstrap.OpenPostgres(ctx, o.DSN)
	if err != nil {
		return fail("isolated_database_unavailable")
	}
	defer pool.Close()
	if err = pool.QueryRow(ctx, `SHOW server_version`).Scan(&r.PostgresVersion); err != nil {
		return fail("database_version_unavailable")
	}
	if err = pool.QueryRow(ctx, `SELECT extversion FROM pg_extension WHERE extname='vector'`).Scan(&r.PgvectorVersion); err != nil {
		return fail("pgvector_unavailable")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fail("fixture_transaction_failed")
	}
	defer func() {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer rollbackCancel()
		if err := tx.Rollback(rollbackCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			r.Status = "failed"
			r.Failure = "fixture_rollback_failed"
			runErr = errors.New("memory fixture rollback failed")
		}
	}()
	if err = seedMemoryBenchmark(ctx, tx, c, documentVectors, r.Provider); err != nil {
		return fail("fixture_seed_failed")
	}
	scopes := map[string]memoryBenchScope{}
	for _, s := range c.Scopes {
		scopes[s.ID] = s
	}
	ks := []int{1, 3, 5, 8}
	retriever := pg.NewHybridMemoryRetriever(tx)
	rng := rand.New(rand.NewSource(o.Seed))
	first := map[string]memoryBenchRow{}
	var timings, rejections []float64
	byK := map[int][]float64{}
	for repeat := -1; repeat < o.Repeats; repeat++ {
		for _, i := range rng.Perm(len(c.Questions)) {
			question := c.Questions[i]
			scope := scopes[question.Scope]
			if err = setMemoryBenchPublication(ctx, tx, scope, r.Provider); err != nil {
				return fail("fixture_publication_failed")
			}
			for _, ki := range rng.Perm(len(ks)) {
				k := ks[ki]
				query := memoryBenchmarkQuery(scope, question.Text, queryVectors[i], r.Provider, k)
				start := time.Now()
				found, searchErr := retriever.Search(ctx, query)
				elapsed := float64(time.Since(start)) / float64(time.Millisecond)
				if repeat < 0 {
					r.WarmupSearches++
					continue
				}
				if scope.ExpectError {
					rejections = append(rejections, elapsed)
				} else {
					timings = append(timings, elapsed)
					byK[k] = append(byK[k], elapsed)
				}
				row := evaluateMemoryResult(question, scope, k, found, searchErr)
				r.IsolationViolations += len(row.Unexpected)
				if row.Failure != "" {
					r.SearchFailures++
				}
				key := fmt.Sprintf("%s/%d", question.ID, k)
				if previous, ok := first[key]; ok {
					if strings.Join(previous.Returned, "\x00") != strings.Join(row.Returned, "\x00") || previous.Failure != row.Failure {
						r.UnstableResults++
					}
				} else {
					first[key] = row
					r.Rows = append(r.Rows, row)
				}
			}
		}
	}
	r.Search = memoryDistribution(timings)
	r.RejectedScope = memoryDistribution(rejections)
	sort.Slice(r.Rows, func(i, j int) bool {
		if r.Rows[i].Question == r.Rows[j].Question {
			return r.Rows[i].K < r.Rows[j].K
		}
		return r.Rows[i].Question < r.Rows[j].Question
	})
	for _, k := range ks {
		r.SearchByK = append(r.SearchByK, memoryBenchKLatency{K: k, Search: memoryDistribution(byK[k])})
		metric := memoryBenchRecall{K: k}
		for _, row := range r.Rows {
			if row.K == k && row.Recall != nil {
				metric.Questions++
				metric.MacroRecall += *row.Recall
			}
		}
		if metric.Questions > 0 {
			metric.MacroRecall /= float64(metric.Questions)
		}
		r.Recall = append(r.Recall, metric)
	}
	// Replay the real production supplement/budget path with already measured
	// query vectors. No external model call is made inside the fixture transaction.
	for _, budget := range []int{512, 1024, 2048} {
		metric := memoryBenchBudgetResult{Tokens: budget}
		for i, question := range c.Questions {
			scope := scopes[question.Scope]
			if len(question.Relevant) == 0 || (scope.Owner != "" && scope.Owner != "timeline") {
				continue
			}
			if err = setMemoryBenchPublication(ctx, tx, scope, r.Provider); err != nil {
				return fail("fixture_publication_failed")
			}
			query := memoryBenchmarkQuery(scope, question.Text, queryVectors[i], r.Provider, 8)
			service := memorycontext.Service{TopK: 8, MemoryTokenBudget: budget, Builder: contextbuilder.Builder{
				Embeddings: cachedMemoryBenchQuery{provider: r.Provider, vector: queryVectors[i]}, Memory: retriever,
				Visibility: pg.NewMemoryVisibilityResolver(tx), Profile: query.Profile,
			}}
			out := service.Build(ctx, memorycontext.Request{StoryID: query.StoryID, TimelineID: query.TimelineID, AtEventSeq: query.AtEventSeq, QueryText: question.Text, Authoritative: []byte(`{"fixture":"current state kept separately"}`)})
			var evidence []memory.Memory
			if out != nil {
				evidence = out.Evidence
			}
			row := evaluateMemoryResult(question, scope, 8, evidence, nil)
			metric.Questions++
			metric.MacroRecall += *row.Recall
			metric.MeanRetainedDocuments += float64(len(evidence))
			metric.Rows = append(metric.Rows, row)
			r.IsolationViolations += len(row.Unexpected)
		}
		if metric.Questions > 0 {
			metric.MacroRecall /= float64(metric.Questions)
			metric.MeanRetainedDocuments /= float64(metric.Questions)
		}
		r.ContextBudgets = append(r.ContextBudgets, metric)
	}
	if r.SearchFailures+r.IsolationViolations+r.UnstableResults > 0 {
		return fail("retrieval_correctness_gate_failed")
	}
	r.Status = "measured"
	if !r.SemanticEvaluation {
		r.Status = "smoke_only"
	}
	return nil
}

type cachedMemoryBenchQuery struct {
	provider ai.ProviderIdentity
	vector   []float32
}

func (e cachedMemoryBenchQuery) Identity() ai.ProviderIdentity { return e.provider }
func (e cachedMemoryBenchQuery) Dimensions() int               { return 384 }
func (e cachedMemoryBenchQuery) Capabilities(context.Context) (ai.EmbeddingCapabilities, error) {
	return ai.EmbeddingCapabilities{Dimensions: 384, Model: e.provider.Model}, nil
}
func (e cachedMemoryBenchQuery) EmbedDocuments(context.Context, ai.EmbeddingRequest) (ai.EmbeddingResponse, error) {
	return ai.EmbeddingResponse{}, errors.New("unexpected document embedding during budget replay")
}
func (e cachedMemoryBenchQuery) EmbedQueries(ctx context.Context, r ai.EmbeddingRequest) (ai.EmbeddingResponse, error) {
	if ctx.Err() != nil {
		return ai.EmbeddingResponse{}, ctx.Err()
	}
	if len(r.Texts) != 1 {
		return ai.EmbeddingResponse{}, errors.New("expected single cached query")
	}
	return ai.EmbeddingResponse{Vectors: [][]float32{append([]float32(nil), e.vector...)}}, nil
}
