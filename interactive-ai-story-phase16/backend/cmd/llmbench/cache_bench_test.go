package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheBenchmarkFakePreservesPairsAndUnknownCache(t *testing.T) {
	output := filepath.Join(t.TempDir(), "cache.json")
	if err := runCacheBenchmark(turnBenchmarkOptions{Provider: "fake", Repeats: 2}, output); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(output)
	var report cacheBenchReport
	if json.Unmarshal(raw, &report) != nil || report.Status != "smoke_only" || !report.AcceptancePassed || len(report.Runs) != 24 || len(report.Summary) != 4 {
		t.Fatalf("invalid fake report: %s", raw)
	}
	pairs := map[string][]cacheBenchRow{}
	for _, r := range report.Runs {
		if !r.Passed || r.Call.HTTPPosts != 0 || r.Call.CachedInputTokens != nil {
			t.Fatal("fake cache evidence fabricated")
		}
		if r.ExactInputRepeat != (r.Repeat > 1) {
			t.Fatal("exact repeated input misclassified")
		}
		key := r.Case + string(rune('0'+r.Repeat))
		pairs[key] = append(pairs[key], r)
	}
	for _, p := range pairs {
		if len(p) != 2 || p[0].SemanticSHA256 != p[1].SemanticSHA256 || p[0].WireInputSHA256 == p[1].WireInputSHA256 || p[0].Layout == p[1].Layout {
			t.Fatal("layout comparison changed semantic inputs")
		}
	}
	for _, s := range report.Summary {
		if s.PositiveCache != 0 || s.CacheReported != 0 || s.CacheUnreported != 6 || s.DistinctSuccessfulInputs != 3 {
			t.Fatal("cache counters fabricated")
		}
	}
}
func TestCacheSummaryKeepsFailureAndReportedZeroSeparate(t *testing.T) {
	zero, positive := int64(0), int64(128)
	rows := []cacheBenchRow{
		{Role: "writer", Layout: "current", Passed: true, WireInputSHA256: "a", Call: turnRoleCall{ElapsedMS: 10}},
		{Role: "writer", Layout: "current", Passed: true, WireInputSHA256: "b", Call: turnRoleCall{ElapsedMS: 20, CachedInputTokens: &zero}},
		{Role: "writer", Layout: "current", Passed: true, WireInputSHA256: "c", Call: turnRoleCall{ElapsedMS: 30, CachedInputTokens: &positive}},
		{Role: "writer", Layout: "current", Call: turnRoleCall{ElapsedMS: 100, Failed: true}},
	}
	summary := summarizeCacheRows(rows)[0]
	if summary.Failed != 1 || summary.CacheUnreported != 1 || summary.CacheReported != 2 || summary.PositiveCache != 1 || summary.ReportedZero != 1 || summary.ReportedCachedTokens != 128 || summary.Latency.Count != 4 || summary.Latency.MedianMS != 25 {
		t.Fatalf("failure or cache evidence hidden: %+v", summary)
	}
}

func TestCacheCaseSelectionKeepsRequestedRolesOnly(t *testing.T) {
	output := filepath.Join(t.TempDir(), "selected.json")
	if err := runCacheBenchmark(turnBenchmarkOptions{Provider: "fake", Repeats: 1, Cases: " writer/conversation , writer/danger "}, output); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(output)
	var report cacheBenchReport
	_ = json.Unmarshal(raw, &report)
	if len(report.Runs) != 4 || len(report.Summary) != 2 {
		t.Fatal("case selection ignored")
	}
	for _, row := range report.Runs {
		if row.Role != "writer" || row.Case == "writer/inspection" {
			t.Fatal("unselected case entered comparison")
		}
	}
}
