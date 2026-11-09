package main

import (
	"os"
	"strings"
	"testing"
)

func TestAdaptiveWriterCandidateKeepsSafetyAndRemovesForcedNovelty(t *testing.T) {
	raw, err := os.ReadFile("fixtures/writer-adaptive-length-v5.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{"100-220 Russian words", "300-500 Russian words", "previousBeatText", "eight or more consecutive words", "exact balance", "no significant change", "Do not make or state a different voluntary strategic choice", "worn equipment", "incidental clothing", "negotiating or forming an agreement", "Do not stop immediately before it", "keep the view beyond indistinct", "appears promising", "Explicit established facts override generic caution", "exact canonical name once"} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing writer contract: %s", required)
		}
	}
	if strings.Contains(text, "Every beat must create at least two observable state changes") {
		t.Fatal("fixed novelty requirement remains in adaptive candidate")
	}
}
