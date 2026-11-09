package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scheduleFixture() turnBenchmarkRun {
	r := turnBenchmarkRun{Succeeded: true, ElapsedMS: 60, Pair: "test/1"}
	for _, c := range []struct {
		role       string
		start, end float64
	}{
		{"turn_planner", 0, 2}, {"writer", 3, 13}, {"world_evaluator", 14, 54},
		{"state_evaluator", 14, 15}, {"quest_evaluator", 14, 34}, {"choices", 35, 37},
	} {
		r.Calls = append(r.Calls, turnRoleCall{Role: c.role, StartMS: c.start, EndMS: c.end, ElapsedMS: c.end - c.start})
	}
	return r
}
func TestTurnScheduleUsesDependencyFloorAndOverlappingCalls(t *testing.T) {
	r := analyzeTurnSchedule(scheduleFixture(), true)
	if !r.Eligible || r.CriticalBranch != "world" || r.ObservedPostWriterMS != 41 || r.SerialCallSumMS != 63 || r.DependencyFloorMS != 40 || r.SchedulingGapMS != 1 || r.OverlapVersusSerialMS != 22 || r.IndependentChoicesUpperBoundMS != 0 {
		t.Fatalf("wrong world-critical arithmetic: %+v", r)
	}
	run := scheduleFixture()
	run.Calls[2].EndMS = 30
	run.Calls[2].ElapsedMS = 16
	r = analyzeTurnSchedule(run, true)
	if !r.Eligible || r.CriticalBranch != "state_quest_choices" || r.DependencyFloorMS != 22 || r.SchedulingGapMS != 2 || r.IndependentChoicesUpperBoundMS != 2 {
		t.Fatalf("wrong choices-critical arithmetic: %+v", r)
	}
}
func TestTurnScheduleExcludesUnsupportedOrInvalidEvidence(t *testing.T) {
	tests := []struct {
		name, reason string
		edit         func(*turnBenchmarkRun)
	}{
		{"failed", "failed_turn", func(r *turnBenchmarkRun) { r.Succeeded = false }},
		{"fallback", "fallback_used", func(r *turnBenchmarkRun) { r.Stages.ChoicesFallbackUsed = true }},
		{"repair", "failed_repaired_or_escalated_call", func(r *turnBenchmarkRun) { r.Calls[3].Repair = true }},
		{"escalated", "failed_repaired_or_escalated_call", func(r *turnBenchmarkRun) { r.Calls[3].EscalatedFrom = "fast" }},
		{"repeated", "repeated_role", func(r *turnBenchmarkRun) { r.Calls[5].Role = "writer" }},
		{"missing", "requires_six_single_calls", func(r *turnBenchmarkRun) { r.Calls = r.Calls[:5] }},
		{"unexpected", "unexpected_roles", func(r *turnBenchmarkRun) { r.Calls[5].Role = "other" }},
		{"zero duration", "invalid_call_bounds", func(r *turnBenchmarkRun) { r.Calls[5].EndMS = r.Calls[5].StartMS; r.Calls[5].ElapsedMS = 0 }},
		{"duration", "invalid_call_bounds", func(r *turnBenchmarkRun) { r.Calls[5].ElapsedMS = 100 }},
		{"early choices", "dependency_violation", func(r *turnBenchmarkRun) { r.Calls[5].StartMS = 30; r.Calls[5].EndMS = 32 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := scheduleFixture()
			tt.edit(&run)
			r := analyzeTurnSchedule(run, true)
			if r.Eligible || r.Reason != tt.reason {
				t.Fatal(r)
			}
		})
	}
	if r := analyzeTurnSchedule(scheduleFixture(), false); r.Eligible || r.Reason != "unsupported_chain_or_provider" {
		t.Fatal(r)
	}
	summary := summarizeTurnSchedule([]turnScheduleRow{analyzeTurnSchedule(scheduleFixture(), true), analyzeTurnSchedule(scheduleFixture(), false)})
	if summary[0].Eligible != 1 || summary[0].Excluded != 1 || summary[0].SchedulingGap.Count != 1 || summary[1].SchedulingGap.Count != 0 {
		t.Fatal(summary)
	}
}
func TestTurnScheduleOfflineReportPreservesSourceAndRejectsPartialRuns(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "in.json"), filepath.Join(dir, "out.json")
	source := turnBenchmarkReport{Mode: "paired-full-turn-v1", Status: "measured", CleanupVerified: true, Planner: true, SafeHybrid: true, Provider: "configured", Runs: []turnBenchmarkRun{scheduleFixture()}}
	source.Runs[0].Text = "PRIVATE_SYNTHETIC_PROSE"
	raw, _ := json.Marshal(source)
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeTurnScheduleAnalysis(input, output); err != nil {
		t.Fatal(err)
	}
	result, _ := os.ReadFile(output)
	var report turnScheduleReport
	if json.Unmarshal(result, &report) != nil || len(report.SourceSHA256) != 64 || report.Summary[0].Eligible != 1 || strings.Contains(string(result), "PRIVATE_SYNTHETIC_PROSE") {
		t.Fatal(string(result))
	}
	if err := writeTurnScheduleAnalysis(input, input); err == nil {
		t.Fatal("source overwrite allowed")
	}
	alias := filepath.Join(dir, "alias.json")
	if err := os.Link(input, alias); err != nil {
		t.Fatal(err)
	}
	if err := writeTurnScheduleAnalysis(input, alias); err == nil {
		t.Fatal("hard-link source overwrite allowed")
	}
	symlink := filepath.Join(dir, "symlink.json")
	if err := os.Symlink(input, symlink); err != nil {
		t.Fatal(err)
	}
	if err := writeTurnScheduleAnalysis(input, symlink); err == nil {
		t.Fatal("symlink source overwrite allowed")
	}
	after, _ := os.ReadFile(input)
	if string(after) != string(raw) {
		t.Fatal("source evidence changed")
	}
	for _, status := range []string{"running", "failed", ""} {
		source.Status = status
		data, _ := json.Marshal(source)
		_ = os.WriteFile(input, data, 0600)
		if err := writeTurnScheduleAnalysis(input, output); err == nil {
			t.Fatalf("partial status %s accepted", status)
		}
	}
	source.Status = "measured"
	source.CleanupVerified = false
	data, _ := json.Marshal(source)
	_ = os.WriteFile(input, data, 0600)
	if err := writeTurnScheduleAnalysis(input, output); err == nil {
		t.Fatal("unverified cleanup accepted")
	}
}
