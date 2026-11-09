package main

import (
	"encoding/json"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

func TestJournalStateMutations(t *testing.T) {
	entity := "00000000-0000-4000-8000-000000000005"
	initial := []generationtarget.JournalEntry{{ID: entity, Category: "item", Status: "active", Quantity: 2}}
	for _, tc := range []struct {
		name, kind, category, status string
		quantity                     int
		exists, want                 bool
	}{
		{"consume one", "journal_entry_updated", "item", "active", 1, true, true},
		{"remove", "journal_entry_updated", "item", "inactive", 0, true, true},
		{"unknown item", "journal_entry_updated", "item", "active", 1, false, false},
		{"category change", "journal_entry_updated", "currency", "active", 1, true, false},
		{"negative", "journal_entry_updated", "item", "active", -1, true, false},
		{"inactive quantity", "journal_entry_updated", "item", "inactive", 2, true, false},
		{"empty active item", "journal_entry_updated", "item", "active", 0, true, false},
		{"duplicate create", "journal_entry_created", "item", "active", 1, true, false},
		{"new item", "journal_entry_created", "item", "active", 1, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]any{"entryId": entity, "name": "Канат", "category": tc.category, "status": tc.status, "quantity": tc.quantity})
			var before []generationtarget.JournalEntry
			if tc.exists {
				before = initial
			}
			if got := validJournalState([]eventResult{{Type: tc.kind, Payload: payload}}, before); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestObjectiveStateMutations(t *testing.T) {
	entity := "00000000-0000-4000-8000-000000000005"
	for _, tc := range []struct {
		name, kind, status string
		progress           int
		exists, want       bool
	}{
		{"progress", "objective_updated", "active", 50, true, true},
		{"complete", "objective_updated", "completed", 100, true, true},
		{"premature status", "objective_updated", "completed", 50, true, false},
		{"unknown", "objective_updated", "active", 50, false, false},
		{"regression", "objective_updated", "active", 10, true, false},
		{"failed progress change", "objective_updated", "failed", 50, true, false},
		{"failed", "objective_updated", "failed", 20, true, true},
		{"historical create", "objective_created", "completed", 100, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]any{"objectiveId": entity, "scope": "global", "successCriteria": "Дверь открыта", "status": tc.status, "progress": tc.progress})
			var before []generationtarget.Objective
			if tc.exists {
				before = []generationtarget.Objective{{ID: entity, Scope: "global", Status: "active", Progress: 20}}
			}
			if got := validObjectiveState([]eventResult{{Type: tc.kind, Payload: payload}}, before); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
