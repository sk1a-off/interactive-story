package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type semanticValidator struct {
	Kind string `json:"kind"`
}

func validateCase(events []eventResult, item benchmarkCase, target generationtarget.Target) map[string]bool {
	results := validateGoldenDeltas(events, item.GoldenDurableDeltas)
	for _, validator := range item.Validators {
		passed := false
		switch validator.Kind {
		case "scene_transition":
			passed = validSceneTransition(events, target.SceneID.String())
		case "journal_state":
			passed = validJournalState(events, target.Journal)
		case "objective_state":
			passed = validObjectiveState(events, target.Objectives)
		case "objectives_remain_active":
			passed = objectivesRemainActive(events, target.Objectives)
		case "objective_progress_unchanged":
			passed = objectiveProgressUnchanged(events, target.Objectives)
		}
		results["semantic:"+validator.Kind] = passed
	}
	for index, expectation := range item.ExpectedMutations {
		results[fmt.Sprintf("expected:%d:%s:%s", index, expectation.EventType, expectation.EntityID)] = expectation.matches(events)
	}
	return results
}

// A transition must close the snapshot's scene, start a distinct valid scene,
// and commit exactly one first-position Beat there, in lifecycle order.
func validSceneTransition(events []eventResult, originalScene string) bool {
	completed, started, beat := -1, -1, -1
	newScene := ""
	for index, event := range events {
		if event.Type != "scene_completed" && event.Type != "scene_started" && event.Type != "beat_committed" {
			continue
		}
		var payload struct {
			SceneID  string `json:"sceneId"`
			Position int    `json:"position"`
		}
		if json.Unmarshal(event.Payload, &payload) != nil {
			return false
		}
		parsed, err := id.Parse(payload.SceneID)
		if err != nil || parsed.IsZero() {
			return false
		}
		switch event.Type {
		case "scene_completed":
			if completed >= 0 || payload.SceneID != originalScene {
				return false
			}
			completed = index
		case "scene_started":
			if started >= 0 || completed < 0 || payload.SceneID == originalScene {
				return false
			}
			started, newScene = index, payload.SceneID
		case "beat_committed":
			if beat >= 0 || started < 0 || payload.SceneID != newScene || payload.Position != 1 {
				return false
			}
			beat = index
		}
	}
	return completed >= 0 && started > completed && beat > started
}

func runValid(run runResult) bool {
	if run.Error != "" || strings.TrimSpace(run.Text) == "" || len(run.Choices) != 4 {
		return false
	}
	seen := map[string]bool{}
	for _, choice := range run.Choices {
		key := strings.ToLower(strings.Join(strings.Fields(choice), " "))
		if key == "" || seen[key] {
			return false
		}
		seen[key] = true
	}
	for _, passed := range run.ValidatorResults {
		if !passed {
			return false
		}
	}
	return true
}

func reportValidationError(report benchmarkReport) error {
	if len(report.Runs) == 0 {
		return fmt.Errorf("benchmark has no candidate runs")
	}
	for _, group := range [][]runResult{report.Runs, report.BaselineRuns} {
		for _, run := range group {
			for _, call := range run.Calls {
				if probe := call.QuestContextProbe; probe != nil && (probe.Error != "" || !probe.ChangesArray) {
					return fmt.Errorf("quest context probe failed for case %q; report saved", run.CaseID)
				}
			}
			if !runValid(run) {
				return fmt.Errorf("benchmark validation failed for case %q; report saved", run.CaseID)
			}
		}
	}
	return nil
}
