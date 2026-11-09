package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRevalidationPreservesSourceAndExposesMissingProgress(t *testing.T) {
	set, err := loadCaseSet("fixtures/durable-resources-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	item := set.Cases[2]
	run := runResult{CaseID: item.ID, Action: item.Action, Text: "Первую закрепил, вторую оставил.", Choices: []string{"A", "B", "C", "D"}, Events: []eventResult{{Type: "beat_committed"}}, ValidatorResults: map[string]bool{"semantic:objectives_remain_active": true}}
	original := benchmarkReport{CaseSet: set.Name, CaseSetRevision: 1, Runs: []runResult{run}, BaselineRuns: []runResult{run}}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "source.json")
	output := filepath.Join(t.TempDir(), "checked.json")
	if err := os.WriteFile(source, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	checked, err := revalidateSavedReport(source, "fixtures/durable-resources-v1.json", output)
	if err != nil {
		t.Fatal(err)
	}
	if checked.CaseSetRevision != 2 || checked.Revalidation == nil || checked.Revalidation.SourceCaseSetRevision != 1 || checked.Revalidation.SourceReport != source {
		t.Fatalf("missing revalidation provenance: %+v", checked.Revalidation)
	}
	if runValid(checked.Runs[0]) || runValid(checked.BaselineRuns[0]) || reportValidationError(checked) == nil {
		t.Fatal("missing progress incorrectly passed")
	}
	if !checked.Runs[0].ValidatorResults["semantic:objectives_remain_active"] {
		t.Fatal("negative-control safety result changed")
	}
	if _, err := revalidateSavedReport(source, "fixtures/durable-resources-v1.json", source); err == nil {
		t.Fatal("source overwrite accepted")
	}
	after, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("source modified")
	}
	if _, err := revalidateSavedReport(source, "fixtures/durable-resources-v1.json", output); err == nil {
		t.Fatal("existing output overwrite accepted")
	}
	if _, err := os.ReadFile(output); err != nil {
		t.Fatal("failed gate report not saved")
	}
}
