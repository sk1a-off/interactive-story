package main

import "testing"

func TestWorldAcceptanceNegativeGates(t *testing.T) {
	set, err := loadCaseSet("fixtures/world-acceptance-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Cases) != 4 {
		t.Fatalf("cases=%d", len(set.Cases))
	}
	for _, c := range set.Cases {
		if c.Category != "world-negative" {
			continue
		}
		base := []eventResult{{Type: "beat_committed"}}
		good := validateGoldenDeltas(base, c.GoldenDurableDeltas)
		if !good["character_world_upserted"] || !good["location_world_upserted"] {
			t.Fatal("no-change case rejected")
		}
		for _, kind := range []string{"character_world_upserted", "location_world_upserted"} {
			bad := validateGoldenDeltas(append(base, eventResult{Type: kind}), c.GoldenDurableDeltas)
			if bad[kind] {
				t.Fatalf("unexpected %s accepted", kind)
			}
		}
	}
}
