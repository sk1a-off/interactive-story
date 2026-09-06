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

func TestApplyPromptUpgradesRemovesOnlyDeprecatedWorldRuleText(t *testing.T) {
	prompts := map[string]string{
		"writer":          "custom opening\n\n- Obey every supplied established `worldRules` card. Apply its prerequisites, energy source, costs, limits, progression and exceptions exactly; never let the neural interface create energy or grant an unlearned ability unless an explicit rule permits it.\n- `feasibility` is authoritative for possible outcomes. Player intent is an attempt, so failure and partial success are preferable to breaking a hard law.\n- Hidden or mystery rules may affect events but must not be explained to the protagonist without observable discovery.\n- When `loreViolations` and `rejectedDraft` are supplied, replace the whole draft once and repair every cited violation without recapping the previous beat.",
		"quest_evaluator": "custom quest rules",
	}
	if !applyPromptUpgrades(prompts) {
		t.Fatal("expected deprecated prompt migration")
	}
	if !strings.Contains(prompts["writer"], "custom opening") {
		t.Fatal("custom prompt text was removed")
	}
	if strings.Contains(prompts["writer"], "worldRules") || strings.Contains(prompts["writer"], "loreViolations") {
		t.Fatal("deprecated world-rule instructions remain")
	}
	if applyPromptUpgrades(prompts) {
		t.Fatal("prompt rollback must be idempotent")
	}
}
