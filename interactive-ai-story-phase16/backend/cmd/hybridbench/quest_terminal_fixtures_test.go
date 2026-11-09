package main

import (
	"encoding/json"
	"testing"
)

func TestTerminalQuestGates(t *testing.T) {
	fixtures := terminalQuestFixtures()
	for _, f := range fixtures {
		if len(f.ExpectedOperations) == 0 {
			if err := checkQuestFixture(f, json.RawMessage(`{"changes":[]}`)); err != nil {
				t.Fatalf("no-op rejected for %s: %v", f.ID, err)
			}
			continue
		}
		if checkQuestFixture(f, json.RawMessage(`{"changes":[]}`)) == nil {
			t.Fatalf("empty response accepted: %s", f.ID)
		}
	}
	complete := json.RawMessage(`{"changes":[{"operation":"complete","objectiveId":"00000000-0000-4000-8000-000000000201","evidenceQuote":"Обе повозки надёжно закреплены канатами."}]}`)
	if err := checkQuestFixture(fixtures[0], complete); err != nil {
		t.Fatal(err)
	}
	wrongOperation := json.RawMessage(`{"changes":[{"operation":"progress","objectiveId":"00000000-0000-4000-8000-000000000201","status":"completed","progress":100,"evidenceQuote":"Обе повозки надёжно закреплены канатами."}]}`)
	if checkQuestFixture(fixtures[0], wrongOperation) == nil {
		t.Fatal("recorded progress=100 regression accepted")
	}
	if checkQuestFixture(fixtures[2], complete) == nil {
		t.Fatal("parent completed while child active")
	}
	failure := json.RawMessage(`{"changes":[{"operation":"fail","objectiveId":"00000000-0000-4000-8000-000000000201","evidenceQuote":"Письмо не доставлено до заката, адресат погиб."}]}`)
	if err := checkQuestFixture(fixtures[1], failure); err != nil {
		t.Fatal(err)
	}
}

func TestFrozenDoorEvidence(t *testing.T) {
	f := terminalQuestFixtures()[3]
	for _, tc := range []struct {
		quote    string
		accepted bool
	}{
		{"Потянув полотно на себя, Алекс заставил его плавно описать широкую дугу, пока край двери с глухим стуком не коснулся резинового упора у плинтуса.", false},
		{"Тяжелая створка теперь замерла у стены в крайнем открытом положении, окончательно освободив дверной проем и оставив позади привычный коридор.", true},
	} {
		raw, err := json.Marshal(map[string]any{"changes": []any{map[string]any{"operation": "complete", "objectiveId": "00000000-0000-4000-8000-000000000201", "evidenceQuote": tc.quote}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := checkQuestFixture(f, raw); (err == nil) != tc.accepted {
			t.Fatalf("accepted=%v, error=%v", tc.accepted, err)
		}
	}
}
