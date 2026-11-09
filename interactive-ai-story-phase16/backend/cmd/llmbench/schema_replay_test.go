package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestSchemaFrozenOutputsReachCanonValidation(t *testing.T) {
	probes, err := schemaProbes()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range probes {
		if p.Expected != "negative" {
			continue
		}
		row := replaySchemaEvaluator(p, schemaBenchRow{Case: p.ID, Output: `{"changes":[]}`})
		if !row.Passed {
			t.Fatalf("negative replay failed: %+v", row)
		}
	}
}
func TestSchemaLiveReportThroughProductionValidators(t *testing.T) {
	path := os.Getenv("SCHEMA_PROBE_REPORT")
	if path == "" {
		t.Skip("set SCHEMA_PROBE_REPORT to a completed synthetic role benchmark")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var source schemaBenchReport
	if json.Unmarshal(raw, &source) != nil || source.Status != "measured" {
		t.Fatal("completed measured report required")
	}
	probes, err := schemaProbes()
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]schemaProbe{}
	for _, p := range probes {
		by[p.ID] = p
	}
	type replayReport struct {
		SourceSHA256 string            `json:"sourceSha256"`
		Rows         []schemaReplayRow `json:"rows"`
		Notes        []string          `json:"notes"`
	}
	hash := sha256.Sum256(raw)
	result := replayReport{SourceSHA256: hex.EncodeToString(hash[:]), Notes: []string{"Offline replay of live evaluator outputs through production Pipeline validation and in-memory Canon capture; no new provider requests or SQL writes.", "The other roles are scripted. Frozen positive/negative mutation counts do not validate all semantic content, branch leakage, or literary quality."}}
	for _, row := range source.Runs {
		p, ok := by[row.Case]
		if !ok {
			t.Fatal("unknown frozen case")
		}
		if p.Expected == "" {
			continue
		}
		h := sha256.Sum256(p.Request.Input)
		if hex.EncodeToString(h[:]) != row.InputSHA256 {
			t.Fatal("source input hash differs from replay fixture")
		}
		result.Rows = append(result.Rows, replaySchemaEvaluator(p, row))
	}
	if len(result.Rows) != 24 {
		t.Fatalf("incomplete evaluator coverage: %d", len(result.Rows))
	}
	if output := os.Getenv("SCHEMA_REPLAY_OUTPUT"); output != "" {
		inputInfo, _ := os.Stat(path)
		outputInfo, _ := os.Stat(output)
		if outputInfo != nil && os.SameFile(inputInfo, outputInfo) {
			t.Fatal("replay output aliases source")
		}
		body, _ := json.MarshalIndent(result, "", "  ")
		if err := os.WriteFile(output, append(body, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range result.Rows {
		if !row.Passed {
			t.Errorf("%s repeat=%d schema=%t: %s mutations=%d", row.Case, row.Repeat, row.Schema, row.Failure, row.Mutations)
		}
	}
}
