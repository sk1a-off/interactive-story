package main

import "testing"

func TestMeasureRoleInputReportsSizesWithoutPersistingInput(t *testing.T) {
	raw := []byte(`{"action":"open","objectives":[{"id":"one"}],"context":{"objectives":[{"id":"one"}],"recentBeats":[{"text":"old"}]}}`)
	bytes, tokens, duplicates, breakdown := measureRoleInput(raw)
	if bytes != len(raw) || tokens <= 0 {
		t.Fatalf("unexpected totals: bytes=%d tokens=%d", bytes, tokens)
	}
	if duplicates != len(`[{"id":"one"}]`) {
		t.Fatalf("unexpected exact duplicate size: %d", duplicates)
	}
	if breakdown["context"] == 0 || breakdown["objectives"] == 0 || breakdown["action"] == 0 {
		t.Fatalf("missing top-level breakdown: %#v", breakdown)
	}
}

func TestMeasureRoleInputAcceptsNonObjectInput(t *testing.T) {
	raw := []byte(`not-json`)
	bytes, tokens, duplicates, breakdown := measureRoleInput(raw)
	if bytes != len(raw) || tokens <= 0 || duplicates != 0 || len(breakdown) != 0 {
		t.Fatalf("unexpected fallback measurement: %d %d %d %#v", bytes, tokens, duplicates, breakdown)
	}
}

func TestSafeProfileKeepsCanonEvaluatorsOnQualityModel(t *testing.T) {
	roles, err := fastRolesForProfile("baseline-safe")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"world_evaluator", "state_evaluator", "quest_evaluator", "writer", "director"} {
		if roles[role] {
			t.Fatalf("canon-affecting role %q routed to fast model", role)
		}
	}
	for _, role := range []string{"action_interpreter", "pacing", "choices"} {
		if !roles[role] {
			t.Fatalf("safe fast role %q missing", role)
		}
	}
}

func TestPercentileUsesNearestRank(t *testing.T) {
	values := []int64{100, 10, 40, 20, 30}
	if percentile(values, 50) != 30 || percentile(values, 95) != 100 || percentile(values, 100) != 100 {
		t.Fatalf("unexpected percentiles: p50=%d p95=%d max=%d", percentile(values, 50), percentile(values, 95), percentile(values, 100))
	}
}

func TestGoldenDeltaValidation(t *testing.T) {
	results := validateGoldenDeltas([]eventResult{{Type: "beat_committed"}, {Type: "choices_ready"}}, []goldenDelta{{EventType: "beat_committed", Minimum: 1, Maximum: 1}, {EventType: "objective_updated", Minimum: 0, Maximum: 0}})
	if !results["beat_committed"] || !results["objective_updated"] {
		t.Fatalf("unexpected validation: %#v", results)
	}
}

func TestOfficialCaseSetCoversRequiredCategories(t *testing.T) {
	set, err := loadCaseSet("fixtures/generation-workflow-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	categories := map[string]bool{}
	for _, item := range set.Cases {
		categories[item.Category] = true
		if len(item.ExpectedImportantFacts) == 0 || len(item.GoldenDurableDeltas) == 0 {
			t.Fatalf("case %s lacks expected facts or durable deltas", item.ID)
		}
	}
	if len(set.Cases) != 12 || len(categories) < 8 {
		t.Fatalf("insufficient fixture coverage: cases=%d categories=%d", len(set.Cases), len(categories))
	}
}

func TestCaseSetRunsMeanRepeatsPerCase(t *testing.T) {
	cases := []benchmarkCase{{ID: "a"}, {ID: "b"}}
	runs := expandCaseRuns(cases, 3)
	if len(runs) != 6 || runs[0].ID != "a" || runs[2].ID != "a" || runs[3].ID != "b" || runs[5].ID != "b" {
		t.Fatalf("unexpected case expansion: %#v", runs)
	}
}

func TestCaseFilterAcceptsIDAndCategory(t *testing.T) {
	cases := []benchmarkCase{{ID: "one", Category: "dialogue"}, {ID: "two", Category: "combat"}}
	filtered, err := filterCases(cases, "one,combat")
	if err != nil || len(filtered) != 2 {
		t.Fatalf("unexpected filter result: %#v %v", filtered, err)
	}
	if _, err = filterCases(cases, "missing"); err == nil {
		t.Fatal("empty case filter result accepted")
	}
}
