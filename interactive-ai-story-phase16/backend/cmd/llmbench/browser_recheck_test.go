package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineBrowserRecheckPreservesSourceAndUpdatesSummary(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.json")
	output := filepath.Join(dir, "checked.json")
	original := report{AverageScore: 0.5, Cases: []caseResult{{Name: "world_browser_negative", Role: "world_evaluator", OK: true, Output: `{"changes":[]}`}}}
	raw, _ := json.Marshal(original)
	if err := os.WriteFile(input, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := recheckBrowserReport(input, output); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := os.ReadFile(input)
	if string(unchanged) != string(raw) {
		t.Fatal("source report changed")
	}
	checked, _ := os.ReadFile(output)
	var wrapper struct {
		SourceSHA256 string `json:"sourceSha256"`
		Report       report `json:"report"`
	}
	if json.Unmarshal(checked, &wrapper) != nil {
		t.Fatal("bad report")
	}
	if wrapper.SourceSHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) || wrapper.Report.AverageScore != 1 {
		t.Fatal("inconsistent hash/summary", wrapper)
	}
	if err := recheckBrowserReport(input, filepath.Join(dir, ".", "source.json")); err == nil {
		t.Fatal("source alias overwrite accepted")
	}
}
