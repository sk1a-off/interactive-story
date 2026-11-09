package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestResourceFixturesAndPartialCompletionGate(t *testing.T) {
	set, err := loadCaseSet("fixtures/durable-resources-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Cases) != 3 {
		t.Fatal("missing resource cases")
	}
	for _, item := range set.Cases {
		if item.Target == nil {
			t.Fatal("fixture depends on live history")
		}
		for _, expected := range item.ExpectedMutations {
			if expected.matches(nil) {
				t.Fatal("missing resource mutation passed")
			}
		}
	}
	item := set.Cases[2]
	if !objectivesRemainActive(nil, item.Target.Objectives) {
		t.Fatal("unchanged active objective rejected")
	}
	for _, tc := range []struct {
		status   string
		progress int
		want     bool
	}{{"active", 50, true}, {"completed", 100, false}, {"failed", 0, false}} {
		payload, _ := json.Marshal(map[string]any{"objectiveId": item.Target.Objectives[0].ID, "scope": "global", "status": tc.status, "progress": tc.progress, "successCriteria": item.Target.Objectives[0].SuccessCriteria})
		got := validateCase([]eventResult{{Type: "objective_updated", Payload: payload}}, item, *item.Target)
		if got["semantic:objectives_remain_active"] != tc.want {
			t.Fatalf("status %s: %v", tc.status, got)
		}
	}
}

func TestRecordedPlannerChildOnlyOutputMissesParentProgress(t *testing.T) {
	set, err := loadCaseSet("fixtures/durable-resources-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	item := set.Cases[2]
	raw, err := os.ReadFile("fixtures/recorded-partial-progress-miss.json")
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Events []eventResult `json:"events"`
	}
	if err := json.Unmarshal(raw, &recorded); err != nil {
		t.Fatal(err)
	}
	if len(recorded.Events) != 1 {
		t.Fatal("recorded counterexample missing")
	}
	if !objectivesRemainActive(recorded.Events, item.Target.Objectives) {
		t.Fatal("recorded output should pass the original safety-only gate")
	}
	if item.ExpectedMutations[0].matches(recorded.Events) {
		t.Fatal("creating a new child hid the missing parent update")
	}
	// Synthetic correction tests the gate, not model behavior; do not force 50.
	for _, progress := range []int{20, 50, 80} {
		payload, _ := json.Marshal(map[string]any{
			"objectiveId": item.Target.Objectives[0].ID, "scope": "global",
			"status": "active", "progress": progress,
			"successCriteria": item.Target.Objectives[0].SuccessCriteria,
		})
		events := append(append([]eventResult(nil), recorded.Events...), eventResult{Type: "objective_updated", Payload: payload})
		if !item.ExpectedMutations[0].matches(events) || !objectivesRemainActive(events, item.Target.Objectives) {
			t.Fatalf("valid partial update rejected: %d", progress)
		}
	}
}
