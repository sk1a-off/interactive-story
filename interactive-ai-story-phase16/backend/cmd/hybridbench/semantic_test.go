package main

import (
	"encoding/json"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
	"testing"
)

func TestSceneTransitionRejectsFormerFalsePass(t *testing.T) {
	oldScene := "00000000-0000-4000-8000-000000000001"
	newScene := "00000000-0000-4000-8000-000000000002"
	event := func(kind, scene string, position int) eventResult {
		payload, _ := json.Marshal(map[string]any{"sceneId": scene, "position": position})
		return eventResult{Type: kind, Payload: payload}
	}
	valid := []eventResult{event("scene_completed", oldScene, 0), event("scene_started", newScene, 0), event("beat_committed", newScene, 1)}
	for name, events := range map[string][]eventResult{
		"valid":            valid,
		"continue scene":   {event("beat_committed", oldScene, 4)},
		"wrong beat scene": {valid[0], valid[1], event("beat_committed", oldScene, 1)},
		"wrong position":   {valid[0], valid[1], event("beat_committed", newScene, 2)},
		"wrong order":      {valid[1], valid[0], valid[2]},
		"duplicate beat":   {valid[0], valid[1], valid[2], valid[2]},
		"same scene":       {valid[0], event("scene_started", oldScene, 0), event("beat_committed", oldScene, 1)},
	} {
		t.Run(name, func(t *testing.T) {
			item := benchmarkCase{GoldenDurableDeltas: []goldenDelta{{EventType: "beat_committed", Minimum: 1, Maximum: 1}}, Validators: []semanticValidator{{Kind: "scene_transition"}}}
			got := validateCase(events, item, generationtarget.Target{SceneID: id.MustParse(oldScene)})
			if got["semantic:scene_transition"] != (name == "valid") {
				t.Fatalf("unexpected result: %v", got)
			}
		})
	}
}

func TestReportGateChecksBothGroupsAndSemanticFailures(t *testing.T) {
	valid := runResult{Text: "Текст", Choices: []string{"A", "B", "C", "D"}}
	if err := reportValidationError(benchmarkReport{Runs: []runResult{valid}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []runResult{
		{Text: " ", Choices: valid.Choices},
		{Text: "Текст", Choices: []string{"A", "a", "C", "D"}},
		{Text: "Текст", Choices: valid.Choices, Error: "failure"},
		{Text: "Текст", Choices: valid.Choices, ValidatorResults: map[string]bool{"semantic:scene_transition": false}},
	} {
		if reportValidationError(benchmarkReport{Runs: []runResult{bad}}) == nil {
			t.Fatal("invalid candidate accepted")
		}
		if reportValidationError(benchmarkReport{Runs: []runResult{valid}, BaselineRuns: []runResult{bad}}) == nil {
			t.Fatal("invalid baseline accepted")
		}
	}
}
