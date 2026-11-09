package main

import (
	"encoding/json"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
	"strings"
	"testing"
)

func TestStateReplayContract(t *testing.T) {
	original := `schema {"evidence":"concrete evidence"} remaining rules`
	fixed, err := correctedStatePrompt(original)
	if err != nil || fixed != `schema {"evidenceQuote":"short verbatim quote copied only from newBeat"} remaining rules` {
		t.Fatalf("%s %v", fixed, err)
	}
	if _, err := correctedStatePrompt(fixed); err == nil {
		t.Fatal("unexpected schema accepted")
	}
	const entryID = "00000000-0000-4000-8000-000000000101"
	for _, tc := range []struct {
		caseID, category, beat, operation, status string
		before, after                             int
	}{
		{"spend-coins-01", "currency", "Осталось семь монет.", "update", "active", 10, 7},
		{"consume-last-ration-01", "item", "Паёк съеден целиком.", "remove", "inactive", 1, 0},
	} {
		journal := []generationtarget.JournalEntry{{ID: entryID, Category: tc.category, Name: "Ресурс", Quantity: tc.before, Status: "active"}}
		raw, _ := json.Marshal(map[string]any{"changes": []any{map[string]any{"operation": tc.operation, "entryId": entryID, "quantity": tc.after, "status": tc.status, "evidenceQuote": tc.beat}}})
		if err := checkStateReplay(tc.caseID, tc.beat, journal, raw); err != nil {
			t.Fatal(err)
		}
		legacy := strings.Replace(string(raw), `"evidenceQuote"`, `"evidence"`, 1)
		if checkStateReplay(tc.caseID, tc.beat, journal, json.RawMessage(legacy)) == nil {
			t.Fatal("legacy evidence accepted")
		}
		if checkStateReplay(tc.caseID, tc.beat, journal, json.RawMessage(`{"changes":[]}`)) == nil {
			t.Fatal("missing mutation accepted")
		}
		if checkStateReplay(tc.caseID, "Действие ещё не произошло.", journal, raw) == nil {
			t.Fatal("non-verbatim evidence accepted")
		}
	}
}

func TestNegativeStateReplay(t *testing.T) {
	journal := []generationtarget.JournalEntry{{ID: "00000000-0000-4000-8000-000000000101", Category: "currency", Name: "Монеты", Quantity: 10, Status: "active"}}
	for _, run := range negativeStateRuns() {
		if err := checkStateReplay(run.CaseID, run.Text, journal, json.RawMessage(`{"changes":[]}`)); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"changes": []any{map[string]any{"operation": "update", "entryId": journal[0].ID, "quantity": 7, "evidenceQuote": run.Text}}})
		if checkStateReplay(run.CaseID, run.Text, journal, raw) == nil {
			t.Fatal("false expense accepted")
		}
	}
}

// Documents the observed all-or-nothing gate; it does not relax production policy.
func TestBadSecondaryEvidenceDropsValidExpense(t *testing.T) {
	journal := []generationtarget.JournalEntry{{ID: "00000000-0000-4000-8000-000000000101", Category: "currency", Name: "Монеты", Quantity: 10, Status: "active"}}
	beat := "у Алекса в дорожном кошеле осталось ровно семь монет. На засаленную стойку со звоном лег массивный кованый ключ с грубой латунной биркой."
	coin := map[string]any{"operation": "update", "entryId": journal[0].ID, "quantity": 7, "evidenceQuote": "у Алекса в дорожном кошеле осталось ровно семь монет"}
	key := map[string]any{"operation": "create", "category": "item", "name": "Ключ", "quantity": 1, "evidenceQuote": "со стойкой со звоном лег массивный кованый ключ с грубой латунной биркой"}
	combined, _ := json.Marshal(map[string]any{"changes": []any{coin, key}})
	if checkStateReplay("spend-coins-01", beat, journal, combined) == nil {
		t.Fatal("bad secondary evidence accepted")
	}
	validOnly, _ := json.Marshal(map[string]any{"changes": []any{coin}})
	if err := checkStateReplay("spend-coins-01", beat, journal, validOnly); err != nil {
		t.Fatal(err)
	}
}
