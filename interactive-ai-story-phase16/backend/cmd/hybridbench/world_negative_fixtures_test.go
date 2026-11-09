package main

import (
	"encoding/json"
	"testing"
)

func TestNegativeWorldFixturesFailClosed(t *testing.T) {
	fixtures := negativeWorldFixtures()
	if len(fixtures) != 7 {
		t.Fatal("wrong count")
	}
	for _, f := range fixtures {
		positive := f.ID == "world-real-damage-control"
		err := checkStructural(f, json.RawMessage(`{"changes":[]}`))
		if (err == nil) == positive {
			t.Fatalf("%s no-op err=%v", f.ID, err)
		}
		raw, _ := json.Marshal(map[string]any{"changes": []any{map[string]any{"type": "upsert_location", "locationId": "00000000-0000-4000-8000-000000000302", "name": "Архив", "description": "Каменное здание с глубокой трещиной, требующее ремонта", "visualAnchorEn": "Stone archive with an oak door", "evidenceQuote": f.Beat}}})
		err = checkStructural(f, raw)
		if (err == nil) != positive {
			t.Fatalf("%s damage err=%v", f.ID, err)
		}
	}
}
