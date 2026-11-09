package main

import (
	"encoding/json"
	"testing"
)

func TestRichQuestFixtureSizesAndGates(t *testing.T) {
	fixtures := richQuestFixtures()
	for _, f := range fixtures {
		compact, err := compactQuestInput(f.Input)
		if err != nil {
			t.Fatal(err)
		}
		if len(compact)*2 >= len(f.Input) {
			t.Fatalf("not a meaningful reduction: %s", f.ID)
		}
		if got := checkQuestFixture(f, json.RawMessage(`{"changes":[]}`)); (got == nil) == f.ExpectProgress {
			t.Fatalf("empty-result gate incorrect: %s %v", f.ID, got)
		}
		if checkQuestFixture(f, json.RawMessage(`{"changes":null}`)) == nil {
			t.Fatal("null changes accepted")
		}
	}
	valid := json.RawMessage(`{"changes":[{"operation":"progress","objectiveId":"00000000-0000-4000-8000-000000000201","scope":"global","kind":"quest","questType":"main","title":"Закрепить обе повозки","successCriteria":"Обе повозки надёжно закреплены канатами.","status":"active","progress":45,"evidenceQuote":"Первая повозка надёжно закреплена."}]}`)
	if err := checkQuestFixture(fixtures[0], valid); err != nil {
		t.Fatal(err)
	}
	if checkQuestFixture(fixtures[1], valid) == nil {
		t.Fatal("historical progress accepted")
	}
	if checkQuestFixture(fixtures[2], valid) == nil {
		t.Fatal("failed-attempt progress accepted")
	}
}
