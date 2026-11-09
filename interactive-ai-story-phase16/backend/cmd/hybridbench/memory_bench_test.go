package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/memory"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

func TestMemoryRecallCountsUniqueRelevantAndKeepsFailuresInDenominator(t *testing.T) {
	q := memoryBenchQuestion{ID: "two", Relevant: []string{"a", "b"}}
	scope := memoryBenchScope{Allowed: []string{"a", "b", "c"}}
	r := evaluateMemoryResult(q, scope, 3, []memory.Memory{{SourceKey: "a"}, {SourceKey: "a"}, {SourceKey: "c"}}, nil)
	if r.Recall == nil || *r.Recall != .5 || len(r.Unexpected) != 1 {
		t.Fatalf("wrong recall/dedup: %+v", r)
	}
	r = evaluateMemoryResult(q, scope, 3, nil, errors.New("database down"))
	if r.Recall == nil || *r.Recall != 0 || r.Failure == "" {
		t.Fatal("failed questions were excluded from recall")
	}
	r = evaluateMemoryResult(q, memoryBenchScope{ExpectError: true}, 3, nil, errors.New("database down"))
	if r.Failure == "" {
		t.Fatal("database error accepted as visibility rejection")
	}
	r = evaluateMemoryResult(memoryBenchQuestion{}, memoryBenchScope{ExpectError: true}, 3, nil, domain.ErrInvalidVisibility)
	if r.Failure != "" {
		t.Fatal("expected visibility rejection failed")
	}
	r = evaluateMemoryResult(memoryBenchQuestion{}, memoryBenchScope{}, 1, []memory.Memory{{SourceKey: "foreign"}}, nil)
	if len(r.Unexpected) != 1 || r.Recall != nil {
		t.Fatal("negative scope failed to detect leak")
	}
}
func TestMemoryLatencyDistributionAndFrozenCorpus(t *testing.T) {
	x := memoryDistribution([]float64{4, 1, 3, 2})
	if x.Count != 4 || x.MedianMS != 2.5 || x.P95MS != 4 || x.MaxMS != 4 {
		t.Fatal(x)
	}
	if memoryDistribution(nil).Count != 0 {
		t.Fatal("empty distribution")
	}
	raw, err := memoryFixtures.ReadFile("fixtures/memory-retrieval-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := loadMemoryCorpus(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Questions) < 30 || len(c.Documents) < 30 {
		t.Fatal("frozen corpus unexpectedly shrank")
	}
	c.Questions[0].Relevant = []string{"missing"}
	raw, _ = json.Marshal(c)
	if _, err = loadMemoryCorpus(raw); err == nil {
		t.Fatal("invalid gold labels accepted")
	}
}

func TestMemoryBenchmarkRejectsMalformedEmbeddingVectors(t *testing.T) {
	v := make([]float32, 384)
	v[0] = 1
	if !validMemoryBenchVectors([][]float32{v}, 1) {
		t.Fatal("valid vector rejected")
	}
	for _, bad := range [][][]float32{nil, {v, v}, {{1}}, {make([]float32, 384)}} {
		if validMemoryBenchVectors(bad, 1) {
			t.Fatal("invalid embedding accepted")
		}
	}
	for _, bad := range []float32{float32(math.NaN()), float32(math.Inf(1))} {
		copyV := append([]float32(nil), v...)
		copyV[0] = bad
		if validMemoryBenchVectors([][]float32{copyV}, 1) {
			t.Fatal("non-finite embedding accepted")
		}
	}
}
func TestMemoryBenchmarkRollsBackFixturesAndMarksFakeResults(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated DATABASE_URL required")
	}
	path := filepath.Join(t.TempDir(), "report.json")
	if err := runMemoryBenchmark(memoryBenchmarkOptions{Provider: "fake", DSN: dsn, Repeats: 1, Seed: 42}, path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report memoryBenchReport
	if err = json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "smoke_only" || report.SemanticEvaluation || report.IsolationViolations != 0 || report.SearchFailures != 0 || len(report.Rows) != report.UniqueQuestions*4 {
		t.Fatalf("unsafe report: %+v", report)
	}
	for _, metric := range report.Recall {
		if metric.Questions != report.UniqueQuestions-4 {
			t.Fatal("repeats or safety negatives inflated semantic sample count")
		}
	}
	if len(report.ContextBudgets) != 3 {
		t.Fatal("budget replay missing")
	}
	for _, metric := range report.ContextBudgets {
		if metric.Questions != report.UniqueQuestions-6 || len(metric.Rows) != metric.Questions {
			t.Fatal("budget replay mixed private/negative scopes into semantic sample")
		}
	}
	pool, err := bootstrap.OpenPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var count int
	if err = pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE username IN ('memorybench-owner','memorybench-foreign-owner')`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("fixture data was committed: %d %v", count, err)
	}
}
func TestMemoryBenchmarkRejectsMissingExplicitDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "failed.json")
	if err := runMemoryBenchmark(memoryBenchmarkOptions{Provider: "fake", Repeats: 1}, path); err == nil {
		t.Fatal("implicit story database accepted")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report memoryBenchReport
	_ = json.Unmarshal(raw, &report)
	if report.Status != "failed" || report.Failure == "" {
		t.Fatal("failure report missing")
	}
}

func TestMemoryBenchmarkRefusesWrongLiveModelIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Error("embedding called after identity mismatch")
		}
		_, _ = w.Write([]byte(`{"model":"different-model","dimensions":384}`))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "identity-failed.json")
	if err := runMemoryBenchmark(memoryBenchmarkOptions{Provider: "user2", DSN: "unused", Endpoint: server.URL, Repeats: 1}, path); err == nil {
		t.Fatal("model mismatch accepted")
	}
	raw, _ := os.ReadFile(path)
	var report memoryBenchReport
	_ = json.Unmarshal(raw, &report)
	if report.Status != "failed" || report.Failure != "embedding_health_identity_mismatch" || len(report.Rows) > 0 {
		t.Fatalf("invalid live report: %+v", report)
	}
}
