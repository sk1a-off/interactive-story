package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
)

func comparisonBaseline(t *testing.T) turnBenchmarkReport {
	t.Helper()
	raw, err := os.ReadFile("../../../docs/implementation/benchmarks/turn-configured-baseline-2026-11-09.json")
	if err != nil {
		t.Fatal(err)
	}
	var report turnBenchmarkReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestHistoricalComparisonPreservesFailuresAndLegacyFingerprintWarning(t *testing.T) {
	before, after := comparisonBaseline(t), comparisonBaseline(t)
	after.PromptSHA256 = strings.Repeat("a", 64)
	after.GenerationProfile = aiconfig.GenerationQuality
	after.Status, after.Failure = "failed", "incomplete_or_failed_turns"
	after.Runs[0].Succeeded = false
	after.Runs[0].Text = "PRIVATE_PROSE"
	after.Runs[0].Calls[0].Failed = true
	after.Runs[0].Calls[0].HTTPPosts++
	after.Runs[0].Calls[0].HTTPStatuses = append(after.Runs[0].Calls[0].HTTPStatuses, 429)
	result, err := compareTurns(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Notes) != 4 || result.AfterHTTP.Posts != 73 || result.AfterHTTP.Statuses[429] != 1 {
		t.Fatalf("legacy warning or failed physical call lost: %+v", result)
	}
	if result.After[0].Failed+result.After[1].Failed != 1 || result.Deltas[0].SuccessfulMatches+result.Deltas[1].SuccessfulMatches != 11 {
		t.Fatal("failed turn hidden or used as a zero-time success")
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "PRIVATE_PROSE") {
		t.Fatal("comparison needlessly copied raw prose")
	}
}

func TestHistoricalComparisonRejectsIncomparableOrIncompleteRuns(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*turnBenchmarkReport)
	}{
		{"running", func(r *turnBenchmarkReport) { r.Status = "running" }},
		{"cleanup", func(r *turnBenchmarkReport) { r.CleanupVerified = false }},
		{"missing turn", func(r *turnBenchmarkReport) { r.Runs = r.Runs[1:] }},
		{"duplicate", func(r *turnBenchmarkReport) { r.Runs[0] = r.Runs[1] }},
		{"seed", func(r *turnBenchmarkReport) { r.Seed++ }},
		{"fixture", func(r *turnBenchmarkReport) { r.FixtureSHA256 = "different" }},
		{"missing prompt identity", func(r *turnBenchmarkReport) { r.PromptID = "" }},
		{"schema", func(r *turnBenchmarkReport) { r.JSONSchema = true }},
		{"profile", func(r *turnBenchmarkReport) { r.GenerationProfile = aiconfig.GenerationBalance }},
		{"prompt revision", func(r *turnBenchmarkReport) { r.PromptRevision++ }},
		{"order", func(r *turnBenchmarkReport) { r.Runs[0].Order++ }},
		{"exposure", func(r *turnBenchmarkReport) {
			for i := range r.Runs {
				if r.Runs[i].Memory {
					r.Runs[i].MemoryDocuments = 0
				}
			}
		}},
		{"time", func(r *turnBenchmarkReport) { r.Runs[0].ElapsedMS = -1 }},
		{"legacy after hash", func(r *turnBenchmarkReport) { r.PromptSHA256 = legacyEmptyPromptHash }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, after := comparisonBaseline(t), comparisonBaseline(t)
			after.PromptSHA256 = strings.Repeat("a", 64)
			tc.change(&after)
			if _, err := compareTurns(before, after); err == nil {
				t.Fatal("unsafe comparison accepted")
			}
		})
	}
	before, after := comparisonBaseline(t), comparisonBaseline(t)
	before.PromptSHA256, after.PromptSHA256 = strings.Repeat("a", 64), strings.Repeat("b", 64)
	if _, err := compareTurns(before, after); err == nil {
		t.Fatal("actual prompt content mismatch accepted")
	}
}

func TestHistoricalComparisonCannotOverwriteInputThroughAlias(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.json")
	if err := os.WriteFile(source, []byte("PRESERVE"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias.json")
	if err := os.Symlink(source, alias); err != nil {
		t.Fatal(err)
	}
	if err := writeTurnComparison(source, source, alias); err == nil {
		t.Fatal("input alias overwritten")
	}
	raw, _ := os.ReadFile(source)
	if string(raw) != "PRESERVE" {
		t.Fatal("source changed")
	}
}

func TestHistoricalComparisonWritesSourceHashesAndRecomputedSummary(t *testing.T) {
	dir := t.TempDir()
	before, after := comparisonBaseline(t), comparisonBaseline(t)
	after.PromptSHA256 = strings.Repeat("a", 64)
	after.Summary = nil // never trust a precomputed report summary
	paths := []string{filepath.Join(dir, "before.json"), filepath.Join(dir, "after.json")}
	for i, report := range []turnBenchmarkReport{before, after} {
		raw, _ := json.Marshal(report)
		if err := os.WriteFile(paths[i], raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(dir, "comparison.json")
	if err := writeTurnComparison(paths[0], paths[1], output); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(output)
	var result turnComparison
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.BeforeSHA256) != 64 || len(result.AfterSHA256) != 64 || result.BeforeSHA256 == result.AfterSHA256 || len(result.After) != 2 || result.After[0].Succeeded+result.After[1].Succeeded != 12 {
		t.Fatal("comparison sources or recomputed summaries missing")
	}
}
