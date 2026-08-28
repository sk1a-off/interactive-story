package bootstrap

import (
	"strings"
	"testing"
)

func TestApplyPromptUpgradesPreservesCustomTextAndIsIdempotent(t *testing.T) {
	prompts := map[string]string{
		"writer":          "custom writer rules",
		"quest_evaluator": "custom quest rules",
	}
	if !applyPromptUpgrades(prompts) {
		t.Fatal("expected prompt upgrade")
	}
	if !strings.HasPrefix(prompts["writer"], "custom writer rules") || !strings.HasPrefix(prompts["quest_evaluator"], "custom quest rules") {
		t.Fatal("custom prompt text was overwritten")
	}
	firstWriter, firstQuest := prompts["writer"], prompts["quest_evaluator"]
	if applyPromptUpgrades(prompts) {
		t.Fatal("second prompt upgrade must be a no-op")
	}
	if prompts["writer"] != firstWriter || prompts["quest_evaluator"] != firstQuest {
		t.Fatal("idempotent upgrade changed prompt text")
	}
}
