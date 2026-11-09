package main

import (
	"encoding/json"
	"testing"
)

func TestExpectedMutationRejectsMissingWrongAndDuplicateUpdates(t *testing.T) {
	quantity := 0
	e := expectedMutation{EventType: "journal_entry_updated", EntityID: "00000000-0000-4000-8000-000000000101", Status: "inactive", Quantity: &quantity}
	valid := eventResult{Type: e.EventType, Payload: json.RawMessage(`{"entryId":"00000000-0000-4000-8000-000000000101","status":"inactive","quantity":0}`)}
	for name, events := range map[string][]eventResult{
		"valid": {valid}, "missing": nil, "duplicate": {valid, valid},
		"missing quantity": {{Type: e.EventType, Payload: json.RawMessage(`{"entryId":"00000000-0000-4000-8000-000000000101","status":"inactive"}`)}},
		"wrong value":      {{Type: e.EventType, Payload: json.RawMessage(`{"entryId":"00000000-0000-4000-8000-000000000101","status":"inactive","quantity":1}`)}},
	} {
		t.Run(name, func(t *testing.T) {
			if e.matches(events) != (name == "valid") {
				t.Fatal("unexpected mutation match")
			}
		})
	}
}

func TestCraftedMutationFixturesRequireActualChanges(t *testing.T) {
	set, err := loadCaseSet("fixtures/durable-mutations-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Cases) != 2 {
		t.Fatal("missing crafted cases")
	}
	for _, item := range set.Cases {
		if item.Target == nil || len(item.ExpectedMutations) == 0 {
			t.Fatal("missing independent state or expected mutation")
		}
		for _, expected := range item.ExpectedMutations {
			if expected.matches(nil) {
				t.Fatal("empty extraction passed")
			}
			payload := map[string]any{"status": expected.Status}
			if expected.Quantity != nil {
				payload["entryId"] = expected.EntityID
				payload["quantity"] = *expected.Quantity
			}
			if expected.Progress != nil {
				payload["objectiveId"] = expected.EntityID
				payload["progress"] = *expected.Progress
			}
			raw, _ := json.Marshal(payload)
			if !expected.matches([]eventResult{{Type: expected.EventType, Payload: raw}}) {
				t.Fatal("expected mutation rejected")
			}
		}
	}
}

func TestExpectedProgressRange(t *testing.T) {
	low, high, exact := 1, 99, 50
	e := expectedMutation{EventType: "objective_updated", EntityID: "00000000-0000-4000-8000-000000000201", Status: "active", MinimumProgress: &low, MaximumProgress: &high}
	for _, progress := range []int{0, 1, 50, 99, 100} {
		raw, _ := json.Marshal(map[string]any{"objectiveId": e.EntityID, "status": "active", "progress": progress})
		if e.matches([]eventResult{{Type: e.EventType, Payload: raw}}) != (progress >= low && progress <= high) {
			t.Fatalf("unexpected range result for %d", progress)
		}
	}
	for name, events := range map[string][]eventResult{
		"missing":          nil,
		"new child only":   {{Type: "objective_created", Payload: json.RawMessage(`{"objectiveId":"00000000-0000-4000-8000-000000000202","parentObjectiveId":"00000000-0000-4000-8000-000000000201","status":"active","progress":0}`)}},
		"missing progress": {{Type: e.EventType, Payload: json.RawMessage(`{"objectiveId":"00000000-0000-4000-8000-000000000201","status":"active"}`)}},
	} {
		if e.matches(events) {
			t.Fatalf("%s passed", name)
		}
	}
	for name, change := range map[string]func(*expectedMutation){
		"mixed exact and range": func(v *expectedMutation) { v.Progress = &exact },
		"missing upper":         func(v *expectedMutation) { v.MaximumProgress = nil },
		"reversed":              func(v *expectedMutation) { v.MinimumProgress, v.MaximumProgress = &high, &low },
		"journal range":         func(v *expectedMutation) { v.EventType, v.Quantity = "journal_entry_updated", &exact },
	} {
		invalid := e
		change(&invalid)
		if invalid.validate() == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}
