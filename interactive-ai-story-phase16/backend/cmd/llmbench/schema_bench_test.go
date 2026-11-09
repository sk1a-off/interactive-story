package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSchemaProbeFrozenInputsAndNegativeChecks(t *testing.T) {
	probes, err := schemaProbes()
	if err != nil || len(probes) != 9 {
		t.Fatalf("invalid fixtures: %v", err)
	}
	for _, p := range probes {
		if p.Expected == "negative" {
			if why := checkSchemaProbe(p, []byte(`{"changes":[]}`)); why != "" {
				t.Fatal(why)
			}
			if why := checkSchemaProbe(p, []byte(`{"changes":[{"evidenceQuote":"Илья остался неподвижен."}]}`)); why != "unsupported_negative_mutation" {
				t.Fatal("negative mutation missed")
			}
		}
		if p.Expected == "positive" && checkSchemaProbe(p, []byte(`{"changes":[]}`)) != "missed_positive_mutation" {
			t.Fatal("missed positive accepted")
		}
	}
	if checkSchemaProbe(schemaProbe{Request: probes[0].Request}, []byte(`[]`)) != "not_json_object" {
		t.Fatal("non-object accepted")
	}
}
func TestSchemaBenchmarkFakeIsExplicitAndPreservesBothArms(t *testing.T) {
	output := filepath.Join(t.TempDir(), "schema.json")
	if err := runSchemaBenchmark(turnBenchmarkOptions{Provider: "fake", Repeats: 2, Seed: 42}, output); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(output)
	var r schemaBenchReport
	if json.Unmarshal(raw, &r) != nil || r.Status != "smoke_only" || len(r.Runs) != 36 || len(r.Summary) != 18 {
		t.Fatalf("incomplete fake report: %s", raw)
	}
	pairs := map[string][]schemaBenchRow{}
	for _, row := range r.Runs {
		if !row.Passed || row.Call.HTTPPosts != 0 {
			t.Fatal("fake bench called provider or failed")
		}
		key := row.Case + string(rune('0'+row.Repeat))
		pairs[key] = append(pairs[key], row)
	}
	for _, pair := range pairs {
		if len(pair) != 2 || pair[0].Schema == pair[1].Schema || pair[0].InputSHA256 != pair[1].InputSHA256 {
			t.Fatal("unpaired inputs")
		}
	}
}
