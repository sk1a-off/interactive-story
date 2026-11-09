package main

import (
	"os"
	"strings"
	"testing"
)

func TestPlannerCandidatePreservesLifecycleWithoutExampleValues(t *testing.T) {
	raw, err := os.ReadFile("fixtures/planner-constraints-v2.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{"nextBeatPosition", "chapterBeatCount", "Copy locationId", "riskFlags", "payment, consumption", "no-change boundary"} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing %s", required)
		}
	}
	for _, placeholder := range []string{"preserved player action", "constraint-preserving immediate goal", `{"intent":`} {
		if strings.Contains(text, placeholder) {
			t.Fatalf("literal example remains: %s", placeholder)
		}
	}
}
