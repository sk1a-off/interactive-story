package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReasoningCasesRespectCurrentFastStateScopeAndAgency(t *testing.T) {
	probes, err := reasoningProbes(true)
	if err != nil || len(probes) != 8 {
		t.Fatalf("wrong frozen set: %v", err)
	}
	basic, err := reasoningProbes(false)
	if err != nil || len(basic) != 4 {
		t.Fatal("fast-state opt-out ignored")
	}
	for _, p := range basic {
		if p.Request.Role == "state_evaluator" {
			t.Fatal("state silently rerouted to fast model")
		}
	}
	for _, p := range probes {
		model := fakeReasoningReply(p, "minimal")
		response, _ := model.Generate(t.Context(), p.Request)
		if why, _ := checkReasoningProbe(p, response.Output); why != "" {
			t.Fatalf("valid frozen reply rejected: %s %s", p.ID, why)
		}
		if p.Request.Role == "turn_planner" {
			if why, _ := checkReasoningProbe(p, []byte(`{"intent":"Ударить Миру","immediateGoal":"Украсть карту","transition":"continue_scene"}`)); why != "planner_required_agency_fallback" {
				t.Fatalf("agency failure masked: %s", why)
			}
		}
		if p.Expected == "negative" {
			if why, _ := checkReasoningProbe(p, []byte(`{"changes":[{"operation":"create","name":"Неполученный ключ"}]}`)); why != "unsupported_negative_mutation" {
				t.Fatal("negative mutation accepted")
			}
		}
	}
}
func TestReasoningFakeBenchmarkPreservesPairsAndCounts(t *testing.T) {
	output := filepath.Join(t.TempDir(), "reasoning.json")
	if err := runReasoningBenchmark(turnBenchmarkOptions{Provider: "fake", FastState: true, Repeats: 3, Seed: 42}, output); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(output)
	var report reasoningBenchReport
	if json.Unmarshal(raw, &report) != nil || report.Status != "smoke_only" || !report.AcceptancePassed || len(report.Runs) != 48 || len(report.Summary) != 16 {
		t.Fatalf("invalid fake report: %s", raw)
	}
	pairs := map[string][]reasoningBenchRow{}
	for _, r := range report.Runs {
		if !r.Passed || r.Call.HTTPPosts != 0 || r.Call.ReasoningEffort != r.Effort || r.Call.ReasoningTokens != nil {
			t.Fatal("invalid fake telemetry")
		}
		key := r.Case + string(rune('0'+r.Repeat))
		pairs[key] = append(pairs[key], r)
	}
	for _, pair := range pairs {
		if len(pair) != 2 || pair[0].InputSHA256 != pair[1].InputSHA256 || pair[0].Effort == pair[1].Effort {
			t.Fatal("pair inputs/arms differ")
		}
	}
	output = filepath.Join(t.TempDir(), "selected.json")
	if err := runReasoningBenchmark(turnBenchmarkOptions{Provider: "fake", FastState: true, Cases: "planner/conversation", Repeats: 1}, output); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(output)
	_ = json.Unmarshal(raw, &report)
	if len(report.Runs) != 2 {
		t.Fatal("case filter ignored")
	}
}
func TestReasoningSummaryRetainsFailuresAndMissingUsage(t *testing.T) {
	rows := []reasoningBenchRow{{Case: "a", Effort: "low", Passed: true, Call: turnRoleCall{ElapsedMS: 10}}, {Case: "a", Effort: "low", Failure: "provider_call_failed", Call: turnRoleCall{Failed: true, ElapsedMS: 50}}, {Case: "a", Effort: "minimal", Failure: "invalid", Call: turnRoleCall{ElapsedMS: 20}}}
	summaries := summarizeReasoningRows(rows)
	if summaries[0].Runs != 2 || summaries[0].Passed != 1 || summaries[0].ProviderFailed != 1 || summaries[0].Latency.Count != 2 || summaries[0].Latency.MedianMS != 30 || summaries[0].ReasoningUsageReported != 0 || summaries[1].ValidationFailed != 1 {
		t.Fatalf("failures/usage hidden: %+v", summaries)
	}
}
