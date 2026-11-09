package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
)

type turnRoundTrip func(*http.Request) (*http.Response, error)

func (f turnRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTurnTransportCountsPollingSeparatelyAndOmitsCredentials(t *testing.T) {
	trace := &turnCallTrace{}
	transport := turnTransport{base: turnRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return nil, errors.New("PRIVATE_TRANSPORT_ERROR")
		}
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("secret"))}, nil
	})}
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		r, _ := http.NewRequestWithContext(context.WithValue(context.Background(), turnTraceKey{}, trace), method, "http://localhost/test", nil)
		r.Header.Set("Authorization", "secret")
		resp, _ := transport.RoundTrip(r)
		if resp != nil {
			resp.Body.Close()
		}
	}
	if trace.Posts != 1 || trace.Gets != 1 || trace.TransportFailures != 1 || len(trace.Statuses) != 1 || trace.Statuses[0] != 429 {
		t.Fatalf("invalid physical counts: %+v", trace)
	}
}
func TestTurnSinkSeparatesWriterOutputFromAvailablePreview(t *testing.T) {
	for _, preview := range []bool{false, true} {
		s := &turnBenchmarkSink{started: time.Now().Add(-time.Second), provisional: preview}
		s.Publish(context.Background(), appgen.Update{Phase: appgen.PhaseWriting, TextDelta: "draft"})
		if s.draft == nil || s.first != nil {
			t.Fatal("writer completion treated as visible text")
		}
		s.Publish(context.Background(), appgen.Update{Phase: appgen.PhaseDraftReady, Provisional: true, TextDelta: "draft"})
		if (s.first != nil) != preview {
			t.Fatal("preview setting ignored")
		}
		s.Publish(context.Background(), appgen.Update{Phase: appgen.PhaseCompleted})
		if s.first == nil || s.ready == nil || *s.first > *s.ready {
			t.Fatal("invalid availability timing")
		}
		if !preview && *s.first != *s.ready {
			t.Fatal("disabled preview exposed early text")
		}
	}
}
func TestTurnSummaryPreservesFailuresAndPairsOnlyEffectiveMemory(t *testing.T) {
	f1, f2, r1, r2 := float64(10), float64(15), float64(20), float64(30)
	runs := []turnBenchmarkRun{{Pair: "a", Succeeded: true, ElapsedMS: 21, FirstAvailableTextMS: &f1, ReadyMS: &r1}, {Pair: "a", Memory: true, MemoryDocuments: 2, Succeeded: true, ElapsedMS: 31, FirstAvailableTextMS: &f2, ReadyMS: &r2}, {Pair: "failed", Memory: true, Succeeded: false, ElapsedMS: 100}}
	s := summarizeTurnRuns(runs)
	if s[1].Runs != 2 || s[1].Failed != 1 || s[1].Total.Count != 1 || s[1].MemorySkipped != 1 {
		t.Fatal("failed turns hidden or counted as zero-time successes")
	}
	d := pairedTurnDeltas(runs)
	if len(d) != 1 || d[0].ReadyMS != 10 || d[0].FirstTextMS != 5 {
		t.Fatal(d)
	}
	runs[1].MemoryDocuments = 0
	if len(pairedTurnDeltas(runs)) != 0 {
		t.Fatal("skipped memory treated as paired exposure")
	}
}
func TestTurnBenchmarkFakeFullPipelineAndCleanup(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated DATABASE_URL required")
	}
	path := filepath.Join(t.TempDir(), "turn.json")
	options := turnBenchmarkOptions{Provider: "fake", EmbeddingProvider: "fake", FixtureDSN: dsn, Repeats: 2, Seed: 42, Planner: true, Provisional: true, FastState: true, JournalIsolation: true}
	if err := runTurnBenchmark(options, path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report turnBenchmarkReport
	if err = json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "smoke_only" || !report.CleanupVerified || len(report.Runs) != 12 || len(report.Paired) != 6 {
		t.Fatalf("incomplete paired smoke: status=%s runs=%d pairs=%d clean=%t", report.Status, len(report.Runs), len(report.Paired), report.CleanupVerified)
	}
	for _, r := range report.Runs {
		if !r.Succeeded || r.Choices != 4 || len(r.Calls) != 6 || r.ReadyMS == nil || r.FirstAvailableTextMS == nil {
			t.Fatalf("incomplete turn: %+v", r)
		}
		if r.Memory && r.MemoryDocuments == 0 {
			t.Fatal("memory never reached model input")
		}
		if !r.Memory && r.MemoryDocuments != 0 {
			t.Fatal("memory leaked into control arm")
		}
		for _, c := range r.Calls {
			if c.HTTPPosts+c.HTTPGets != 0 {
				t.Fatal("fake run made network model calls")
			}
		}
	}
}
