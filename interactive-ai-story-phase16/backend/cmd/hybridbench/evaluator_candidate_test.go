package main

import (
	"os"
	"strings"
	"testing"
)

func TestEvaluatorCandidatesStateTheirCriticalBoundaries(t *testing.T) {
	checks := map[string][]string{
		"fixtures/state-portable-items-v2.md": {"tangible portable object", "agreement, promise, relationship", "location, building, room, shelter", "physically possesses"},
		"fixtures/world-newbeat-quotes-v3.md": {"literal substring check", "exact name", "never copy its exterior wording from older context", "character-for-character"},
		"fixtures/quest-fast-outcome-v3.md":   {"Terminal failure gate", "terminally impossible", "temporary obstacle", `{"changes":[]}`},
	}
	for file, required := range checks {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range required {
			if !strings.Contains(string(raw), fragment) {
				t.Fatalf("%s is missing %q", file, fragment)
			}
		}
	}
}
