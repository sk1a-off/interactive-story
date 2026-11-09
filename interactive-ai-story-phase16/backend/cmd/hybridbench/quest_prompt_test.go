package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
)

func TestQuestPromptOverrideIsLocalAndRecorded(t *testing.T) {
	original := map[string]string{"writer": "unchanged", "quest_evaluator": "old"}
	revision := promptset.Revision{Revision: 18, Prompts: original}
	if meta, err := overrideQuestPrompt(&revision, ""); err != nil || meta != nil {
		t.Fatal("default path must not override the active prompt")
	}
	path := filepath.Join(t.TempDir(), "quest.md")
	raw := []byte("new quest contract\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := overrideQuestPrompt(&revision, path)
	if err != nil {
		t.Fatal(err)
	}
	if meta.File != path || meta.SHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) || revision.Revision != 18 {
		t.Fatal("incorrect override provenance")
	}
	if revision.Prompts["writer"] != "unchanged" || revision.Prompts["quest_evaluator"] != string(raw) || original["quest_evaluator"] != "old" {
		t.Fatal("override changed unrelated/shared prompts")
	}
	if err := os.WriteFile(path, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := overrideQuestPrompt(&revision, path); err == nil {
		t.Fatal("empty prompt accepted")
	}
}

func TestStatePromptOverridePreservesOtherRoles(t *testing.T) {
	original := map[string]string{"writer": "writer-original", "quest_evaluator": "quest-original", "state_evaluator": "state-original"}
	revision := promptset.Revision{Revision: 21, Prompts: original}
	meta, err := overrideRolePrompt(&revision, "fixtures/state-shortquote-v1.md", "state_evaluator")
	if err != nil || meta == nil {
		t.Fatalf("override: %v", err)
	}
	if revision.Prompts["writer"] != original["writer"] || revision.Prompts["quest_evaluator"] != original["quest_evaluator"] || original["state_evaluator"] != "state-original" || revision.Prompts["state_evaluator"] == original["state_evaluator"] {
		t.Fatal("state override mutated shared or unrelated prompts")
	}
}

func TestQuestPromptOverrideComposesFilesInOrder(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "base.md")
	second := filepath.Join(dir, "addon.md")
	if err := os.WriteFile(first, []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("addon\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	revision := promptset.Revision{Prompts: map[string]string{"quest_evaluator": "old"}}
	meta, err := overrideQuestPrompt(&revision, first+","+second)
	if err != nil {
		t.Fatal(err)
	}
	want := "base\n\naddon\n"
	if revision.Prompts["quest_evaluator"] != want || meta.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(want))) {
		t.Fatalf("unexpected composite override: %#v %q", meta, revision.Prompts["quest_evaluator"])
	}
}

// Contract-presence test, not evidence that a live model will obey the prompt.
func TestQuestPromptExplicitlyRequiresParentProgressAndNegativeEvidenceBoundary(t *testing.T) {
	raw, err := os.ReadFile("../../prompts/v1/quest_evaluator.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"Creating a child stage does not replace updating its parent",
		"never assign a fixed 50 percent by default",
		"a failed attempt, historical achievements",
		"Every change requires a short verbatim",
	} {
		if !strings.Contains(string(raw), required) {
			t.Fatalf("missing contract: %s", required)
		}
	}
}
