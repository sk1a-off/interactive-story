package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCorrectedWorldPromptIsNarrow(t *testing.T) {
	original := `prefix {"currentGoal":"..."} {"visualAnchorEn":"English visual anchor"} suffix`
	got, err := correctedWorldPrompt(original)
	if err != nil || strings.Count(got, `"evidenceQuote"`) != 2 || !strings.HasPrefix(got, "prefix ") || !strings.HasSuffix(got, " suffix") {
		t.Fatalf("%s %v", got, err)
	}
	if _, err := correctedWorldPrompt(got); err == nil {
		t.Fatal("already corrected schema accepted")
	}
}

func TestFrozenLighthouseQuoteBoundary(t *testing.T) {
	var f structuralFixture
	if err := json.Unmarshal(frozenLighthouse, &f); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		quote string
		want  bool
	}{
		{"Северного маяка — высокой белой каменной башни с красной металлической крышей", false},
		{"Тяжелый каменный порог остается позади, и глухой звук шагов гулко разносится под высоким сводом нижнего яруса.", true},
	} {
		raw, _ := json.Marshal(map[string]any{"changes": []any{map[string]any{"type": "upsert_location", "name": "Северный маяк", "description": "Каменный маяк, пригодный для убежища", "visualAnchorEn": "White stone tower with a red metal roof", "evidenceQuote": tc.quote}}})
		if err := checkStructural(f, raw); (err == nil) != tc.want {
			t.Fatalf("want=%v err=%v", tc.want, err)
		}
	}
	if checkStructural(f, json.RawMessage(`{"changes":[]}`)) == nil {
		t.Fatal("missing lighthouse accepted")
	}
}

func TestStructuralFixtureAssertions(t *testing.T) {
	fixtures := structuralFixtures()
	if len(fixtures) != 24 {
		t.Fatalf("fixture count %d", len(fixtures))
	}
	for _, f := range fixtures {
		if f.Quest != nil {
			continue
		}
		expectEmpty := len(f.Balances) == 0 && f.WorldType == ""
		if err := checkStructural(f, json.RawMessage(`{"changes":[]}`)); (err == nil) != expectEmpty {
			t.Fatalf("%s empty output: %v", f.ID, err)
		}
		if f.Role == "state_evaluator" && len(f.Balances) > 0 {
			changes := []map[string]any{}
			for entry, quantity := range f.Balances {
				changes = append(changes, map[string]any{"operation": "update", "entryId": entry, "quantity": quantity, "evidenceQuote": f.Beat})
			}
			raw, _ := json.Marshal(map[string]any{"changes": changes})
			if err := checkStructural(f, raw); err != nil {
				t.Fatalf("%s: %v", f.ID, err)
			}
			changes[0]["quantity"] = -1
			raw, _ = json.Marshal(map[string]any{"changes": changes})
			if checkStructural(f, raw) == nil {
				t.Fatal("negative balance accepted")
			}
		}
		if f.Role == "world_evaluator" && f.WorldType != "" {
			change := map[string]any{"type": f.WorldType, "name": f.WorldName, "age": 40, "description": "Высокая белая каменная башня с красной крышей", "role": "Кузнец", "personality": "Деловой", "relationship": "Нейтральное", "visualAnchorEn": "Adult man with a red beard", "evidenceQuote": f.Beat}
			if f.WorldID != "" {
				change["characterId"] = f.WorldID
				change["age"] = 30
				change["role"] = "Хранительница архива"
				change["personality"] = "Спокойная"
				change["visualAnchorEn"] = "Woman with dark braided hair"
				change["mood"] = "Сердита"
				change["currentGoal"] = "Найти пропавшую книгу"
			}
			raw, _ := json.Marshal(map[string]any{"changes": []any{change}})
			if err := checkStructural(f, raw); err != nil {
				t.Fatalf("%s positive: %v", f.ID, err)
			}
			change["evidenceQuote"] = "Этого в тексте нет."
			raw, _ = json.Marshal(map[string]any{"changes": []any{change}})
			if checkStructural(f, raw) == nil {
				t.Fatal("invented evidence accepted")
			}
		}
	}
}
