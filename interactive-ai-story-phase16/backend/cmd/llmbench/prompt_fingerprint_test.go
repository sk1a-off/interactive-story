package main

import (
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
)

func TestPromptFingerprintTracksContentAndSampling(t *testing.T) {
	p := promptset.Revision{Prompts: map[string]string{"writer": "first", "choices": "second"}, RoleSettings: map[string]promptset.RoleSettings{"writer": {MaxTokens: 1000, TopP: .9}}}
	original := promptFingerprint(p)
	if original == "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a" {
		t.Fatal("prompt fingerprint still hashes an empty object")
	}
	p.Revision++
	if promptFingerprint(p) != original {
		t.Fatal("content fingerprint depends on metadata")
	}
	p.Prompts["writer"] = "changed"
	if promptFingerprint(p) == original {
		t.Fatal("prompt content change was invisible")
	}
	p.Prompts["writer"] = "first"
	p.RoleSettings["writer"] = promptset.RoleSettings{MaxTokens: 2000, TopP: .9}
	if promptFingerprint(p) == original {
		t.Fatal("sampling change was invisible")
	}
	p.RoleSettings["writer"] = promptset.RoleSettings{MaxTokens: 1000, TopP: .9}
	p.Prompts = map[string]string{"choices": "second", "writer": "first"}
	if promptFingerprint(p) != original {
		t.Fatal("map insertion order changed content identity")
	}
}
