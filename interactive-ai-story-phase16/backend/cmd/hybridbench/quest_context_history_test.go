package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

// The same current beat can describe either a new achievement or work already
// accounted for. Which wagon is at the gate is recorded only in older prose.
// No credentials or LLM calls are needed to prove the information loss.

func TestPrunedContextLosesAlreadyCountedAchievement(t *testing.T) {
	fixtures := historyIdentityFixtures()
	a, err := compactQuestInput(fixtures[0].Input)
	if err != nil {
		t.Fatal(err)
	}
	b, err := compactQuestInput(fixtures[1].Input)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(fixtures[0].Input, fixtures[1].Input) {
		t.Fatal("full inputs must differ")
	}
	if !bytes.Equal(a, b) {
		t.Fatal("counterexample no longer loses the distinguishing history; review expected behavior")
	}
	if fixtures[0].ExpectProgress == fixtures[1].ExpectProgress {
		t.Fatal("expected outcomes must differ")
	}
	noChange := json.RawMessage(`{"changes":[]}`)
	progress := json.RawMessage(`{"changes":[{"operation":"progress","objectiveId":"00000000-0000-4000-8000-000000000201","status":"active","progress":66,"evidenceQuote":"Повозка у ворот надёжно закреплена канатом."}]}`)
	if err := checkQuestFixture(fixtures[0], noChange); err != nil {
		t.Fatal(err)
	}
	if err := checkQuestFixture(fixtures[1], progress); err != nil {
		t.Fatal(err)
	}
	if checkQuestFixture(fixtures[0], progress) == nil {
		t.Fatal("double-counted progress accepted")
	}
	if checkQuestFixture(fixtures[1], noChange) == nil {
		t.Fatal("lost progress accepted")
	}
	t.Logf("Different expected outcomes collapse to identical input: full=%d/%d bytes compact=%d bytes. Do not activate quest-pruned-v1.", len(fixtures[0].Input), len(fixtures[1].Input), len(a))
}
