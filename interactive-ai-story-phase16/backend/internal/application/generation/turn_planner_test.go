package generation

import (
	"strings"
	"testing"
)

func TestTurnPlanRejectsAgencyDrift(t *testing.T) {
	plan := TurnPlan{Intent: "Взять канаты и привязать фургон к скале", ImmediateGoal: "Закрепить обоз до начала бурана", Transition: "continue_scene"}
	if err := validateTurnPlan("Спокойно расспросить ближайшего собеседника о недавних событиях", plan); err == nil {
		t.Fatal("planner accepted an unrelated substituted action")
	}
}

func TestTurnPlanAcceptsSemanticallyPreservedAction(t *testing.T) {
	plan := TurnPlan{Intent: "Спокойно расспросить ближайшего собеседника о том, что произошло недавно", ImmediateGoal: "Узнать у собеседника подробности недавних событий", Transition: "continue_scene"}
	if err := validateTurnPlan("Спокойно расспросить ближайшего собеседника о недавних событиях", plan); err != nil {
		t.Fatalf("valid preserved action rejected: %v", err)
	}
}

func TestTurnPlanRawActionFallbackPreservesAgency(t *testing.T) {
	action := "Применить доступную герою способность строго в пределах уже установленных ограничений."
	plan := TurnPlan{
		Intent:        "Создать новую разрушительную способность и атаковать ею врага",
		ImmediateGoal: "Проверить доступную силу",
		Transition:    "continue_scene",
	}
	plan.Intent = strings.TrimSpace(plan.Intent)
	if plan.Intent != "" && !intentPreservesAction(action, plan.Intent) {
		plan.Intent = strings.TrimSpace(action)
		plan.IntentFallbackUsed = true
	}
	if !plan.IntentFallbackUsed {
		t.Fatal("agency drift did not activate raw-action fallback")
	}
	if plan.Intent != action {
		t.Fatalf("fallback intent=%q want=%q", plan.Intent, action)
	}
	if err := validateTurnPlan(action, plan); err != nil {
		t.Fatalf("fallback plan rejected: %v", err)
	}
}
