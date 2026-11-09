package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
)

// Analysis is deliberately limited to the six-call planner DAG. Repairs,
// sequential providers and different chains need their own dependency model.
type turnScheduleRow struct {
	Pair                           string  `json:"pair"`
	Memory                         bool    `json:"memory"`
	Eligible                       bool    `json:"eligible"`
	Reason                         string  `json:"reason,omitempty"`
	CriticalBranch                 string  `json:"criticalBranch,omitempty"`
	ObservedPostWriterMS           float64 `json:"observedPostWriterMs"`
	SerialCallSumMS                float64 `json:"serialCallSumMs"`
	DependencyFloorMS              float64 `json:"dependencyFloorMs"`
	SchedulingGapMS                float64 `json:"schedulingGapMs"`
	OverlapVersusSerialMS          float64 `json:"overlapVersusSerialMs"`
	IndependentChoicesUpperBoundMS float64 `json:"independentChoicesUpperBoundMs"`
}
type turnScheduleSummary struct {
	Memory                       bool        `json:"memory"`
	Eligible                     int         `json:"eligible"`
	Excluded                     int         `json:"excluded"`
	WorldCritical                int         `json:"worldCritical"`
	ChoicesBranchCritical        int         `json:"choicesBranchCritical"`
	SchedulingGap                turnTimings `json:"schedulingGap"`
	OverlapVersusSerial          turnTimings `json:"overlapVersusSerial"`
	IndependentChoicesUpperBound turnTimings `json:"independentChoicesUpperBound"`
}
type turnScheduleReport struct {
	SourceSHA256 string                `json:"sourceSha256"`
	SourceStatus string                `json:"sourceStatus"`
	Runs         []turnScheduleRow     `json:"runs"`
	Summary      []turnScheduleSummary `json:"summary"`
	Notes        []string              `json:"notes"`
}

func analyzeTurnSchedule(run turnBenchmarkRun, supported bool) turnScheduleRow {
	r := turnScheduleRow{Pair: run.Pair, Memory: run.Memory}
	reject := func(reason string) turnScheduleRow { r.Reason = reason; return r }
	if !supported {
		return reject("unsupported_chain_or_provider")
	}
	if !run.Succeeded {
		return reject("failed_turn")
	}
	if run.Stages.ChoicesFallbackUsed || run.Stages.ActionIntentFallbackUsed {
		return reject("fallback_used")
	}
	if len(run.Calls) != 6 {
		return reject("requires_six_single_calls")
	}
	calls := make(map[string]turnRoleCall)
	for _, c := range run.Calls {
		if c.Failed || c.Repair || c.EscalatedFrom != "" {
			return reject("failed_repaired_or_escalated_call")
		}
		for _, v := range []float64{c.StartMS, c.EndMS, c.ElapsedMS, run.ElapsedMS} {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				return reject("invalid_call_bounds")
			}
		}
		if c.ElapsedMS <= 0 || run.ElapsedMS <= 0 || c.EndMS < c.StartMS || c.EndMS > run.ElapsedMS || math.Abs(c.EndMS-c.StartMS-c.ElapsedMS) > .001 {
			return reject("invalid_call_bounds")
		}
		if _, exists := calls[c.Role]; exists {
			return reject("repeated_role")
		}
		calls[c.Role] = c
	}
	for _, role := range []string{"turn_planner", "writer", "world_evaluator", "state_evaluator", "quest_evaluator", "choices"} {
		if _, ok := calls[role]; !ok {
			return reject("unexpected_roles")
		}
	}
	planner, writer := calls["turn_planner"], calls["writer"]
	world, state, quest, choices := calls["world_evaluator"], calls["state_evaluator"], calls["quest_evaluator"], calls["choices"]
	if writer.StartMS < planner.EndMS || world.StartMS < writer.EndMS || state.StartMS < writer.EndMS || quest.StartMS < writer.EndMS || choices.StartMS < math.Max(state.EndMS, quest.EndMS) {
		return reject("dependency_violation")
	}
	r.Eligible = true
	r.CriticalBranch = "world"
	if choices.EndMS > world.EndMS {
		r.CriticalBranch = "state_quest_choices"
	}
	r.ObservedPostWriterMS = math.Max(world.EndMS, choices.EndMS) - writer.EndMS
	r.SerialCallSumMS = world.ElapsedMS + state.ElapsedMS + quest.ElapsedMS + choices.ElapsedMS
	r.DependencyFloorMS = math.Max(world.ElapsedMS, math.Max(state.ElapsedMS, quest.ElapsedMS)+choices.ElapsedMS)
	r.SchedulingGapMS = r.ObservedPostWriterMS - r.DependencyFloorMS
	r.OverlapVersusSerialMS = r.SerialCallSumMS - r.ObservedPostWriterMS
	// Counterfactual duration-only bound, not permission to discard dependencies.
	independentFloor := math.Max(math.Max(world.ElapsedMS, state.ElapsedMS), math.Max(quest.ElapsedMS, choices.ElapsedMS))
	r.IndependentChoicesUpperBoundMS = r.DependencyFloorMS - independentFloor
	return r
}

func summarizeTurnSchedule(rows []turnScheduleRow) []turnScheduleSummary {
	var summaries []turnScheduleSummary
	for _, enabled := range []bool{false, true} {
		s := turnScheduleSummary{Memory: enabled}
		var gaps, overlap, choices []float64
		for _, r := range rows {
			if r.Memory != enabled {
				continue
			}
			if !r.Eligible {
				s.Excluded++
				continue
			}
			s.Eligible++
			if r.CriticalBranch == "world" {
				s.WorldCritical++
			} else {
				s.ChoicesBranchCritical++
			}
			gaps = append(gaps, r.SchedulingGapMS)
			overlap = append(overlap, r.OverlapVersusSerialMS)
			choices = append(choices, r.IndependentChoicesUpperBoundMS)
		}
		s.SchedulingGap, s.OverlapVersusSerial, s.IndependentChoicesUpperBound = turnDistribution(gaps), turnDistribution(overlap), turnDistribution(choices)
		summaries = append(summaries, s)
	}
	return summaries
}

func writeTurnScheduleAnalysis(input, output string) error {
	if input == "" || output == "" {
		return errors.New("input report and -output required")
	}
	inPath, e1 := filepath.Abs(input)
	outPath, e2 := filepath.Abs(output)
	if e1 != nil || e2 != nil || inPath == outPath {
		return errors.New("analysis output must differ from input report")
	}
	// Also reject hard-link and symlink aliases, preserving the original evidence.
	inInfo, e1 := os.Stat(input)
	outInfo, e2 := os.Stat(output)
	if e1 != nil {
		return errors.New("input report unavailable")
	}
	if e2 == nil && os.SameFile(inInfo, outInfo) {
		return errors.New("analysis output aliases input report")
	}
	raw, err := os.ReadFile(input)
	if err != nil {
		return errors.New("input report unavailable")
	}
	var source turnBenchmarkReport
	if json.Unmarshal(raw, &source) != nil {
		return errors.New("invalid input report JSON")
	}
	if source.Mode != "paired-full-turn-v1" || (source.Status != "measured" && source.Status != "smoke_only") || source.Failure != "" || !source.CleanupVerified || len(source.Runs) == 0 {
		return errors.New("completed full-turn report with verified cleanup required")
	}
	hash := sha256.Sum256(raw)
	report := turnScheduleReport{SourceSHA256: hex.EncodeToString(hash[:]), SourceStatus: source.Status, Notes: []string{
		"Offline arithmetic on recorded calls; no new provider requests, speed comparison or quality approval.",
		"Only successful single-call six-role planner DAGs with no repairs, escalations or fallbacks are eligible. Exclusions are explicit.",
		"Scheduling gap includes validation, Go work and observer overhead between calls, not just scheduler delay.",
		"Serial call sum is a same-duration counterfactual, not a separately measured serial provider run.",
		"Independent choices bound assumes unchanged durations and discards real journal/quest dependencies; it is not an implemented or safe optimization.",
	}}
	// Fake smoke exercises hosted scheduling mechanically; generic local providers
	// keep their current serial policy and are not modeled as a hosted DAG.
	supported := source.Planner && (source.Provider == "fake" || source.Provider == "configured" && source.SafeHybrid)
	for _, run := range source.Runs {
		report.Runs = append(report.Runs, analyzeTurnSchedule(run, supported))
	}
	report.Summary = summarizeTurnSchedule(report.Runs)
	result, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(output, append(result, '\n'), 0600)
}
