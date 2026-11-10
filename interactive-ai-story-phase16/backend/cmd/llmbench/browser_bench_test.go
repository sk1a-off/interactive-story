package main

import (
	"encoding/json"
	"testing"
)

func TestBrowserSuiteFrozenContractsAndLargeContext(t *testing.T) {
	cases := browserRoleSuite()
	seen := map[string]bool{}
	for _, tc := range cases {
		if seen[tc.Name] {
			t.Fatal("duplicate case", tc.Name)
		}
		seen[tc.Name] = true
		if tc.Name == "writer_large_context" {
			raw, _ := json.Marshal(tc.Input)
			if len(raw) < 20000 || len(raw) > 40000 {
				t.Fatal("large-context size", len(raw))
			}
		}
		if tc.Name == "world_browser_negative" {
			v := tc.Validate(map[string]any{"changes": []any{}}, `{"changes":[]}`)
			if v.SchemaScore != 1 || v.InstructionScore != 1 {
				t.Fatal(v)
			}
		}
		if tc.Name == "world_browser_positive" {
			v := tc.Validate(map[string]any{"changes": []any{}}, `{"changes":[]}`)
			if v.InstructionScore != 0 {
				t.Fatal("empty positive mutation passed")
			}
		}
		if tc.Name == "turn_planner_browser" {
			action := tc.Input.(map[string]any)["action"].(string)
			raw, _ := json.Marshal(map[string]any{"intent": action, "immediateGoal": "Выяснить состояние двери и её петель, сохранив осторожность героя", "transition": "continue_scene", "riskFlags": []any{}, "scene": map[string]any{}, "chapter": map[string]any{}})
			if v := tc.Validate(nil, string(raw)); v.SchemaScore != 1 || v.InstructionScore != 1 {
				t.Fatal("valid planner rejected", v)
			}
		}
		if tc.Name != "browser_escaping" {
			if _, err := browserContract(tc, map[string]string{}); err == nil {
				t.Fatal("missing active prompt accepted")
			}
		}
	}
	if len(cases) != 16 {
		t.Fatal("unexpected fixture count", len(cases))
	}
}

func TestBrowserWorldChecksNormalizedArray(t *testing.T) {
	raw := `{"changes":[{"type":"upsert_location","locationId":"00000000-0000-4000-8000-000000000302","name":"Архив","description":"Каменный фасад повреждён: появилась глубокая трещина, дубовая дверь уцелела.","visualAnchorEn":"Stone archive with a cracked facade and an oak door","evidenceQuote":"На его каменном фасаде появилась свежая глубокая трещина, но дубовая дверь уцелела."}]}`
	for _, tc := range browserRoleSuite() {
		if tc.Name == "world_browser_positive" {
			v := tc.Validate(nil, raw)
			if v.SchemaScore != 1 || v.InstructionScore != 1 || v.ContinuityScore != 1 {
				t.Fatal("normalized production array rejected", v)
			}
			return
		}
	}
	t.Fatal("missing fixture")
}
