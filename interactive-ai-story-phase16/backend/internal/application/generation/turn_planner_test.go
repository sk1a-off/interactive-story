package generation

import "testing"

func TestTurnPlanRejectsAgencyDrift(t *testing.T) {
	plan := TurnPlan{Intent: "Взять канаты и привязать фургон к скале", ImmediateGoal: "Закрепить обоз до начала бурана", Transition: "continue_scene"}
	if err := validateTurnPlan("Спокойно расспросить ближайшего собеседника о недавних событиях", plan); err == nil {
		t.Fatal("planner accepted an unrelated substituted action")
	}
}

func TestTurnPlanAcceptsSemanticallyPreservedAction(t *testing.T) {
	plan := TurnPlan{Intent: "Спокойно расспросить ближайшего собеседника о недавних событиях", ImmediateGoal: "Узнать у собеседника подробности недавних событий", Transition: "continue_scene"}
	if err := validateTurnPlan("Спокойно расспросить ближайшего собеседника о недавних событиях", plan); err != nil {
		t.Fatalf("valid preserved action rejected: %v", err)
	}
}

func TestTurnPlanRawActionFallbackPreservesAgency(t *testing.T) {
	action := "Применить доступную герою способность строго в пределах уже установленных ограничений."
	plan := normalizeTurnPlan(action, TurnPlan{
		Intent:        "Создать новую разрушительную способность и атаковать ею врага",
		ImmediateGoal: "Проверить доступную герою способность в установленных ограничениях",
		Transition:    "continue_scene",
	})
	if !plan.IntentFallbackUsed {
		t.Fatal("agency drift did not activate raw-action fallback")
	}
	if plan.Intent != action || plan.ImmediateGoal != action {
		t.Fatalf("fallback plan=%#v want intent/goal=%q", plan, action)
	}
	if err := validateTurnPlan(action, plan); err != nil {
		t.Fatalf("fallback plan rejected: %v", err)
	}
}

func TestTurnPlanIntentFallbackAlsoDropsHiddenGoal(t *testing.T) {
	action := "Убрать руку с замка, выскочить из-под навеса и помочь Демьяну удерживать коней на ветру"
	plan := normalizeTurnPlan(action, TurnPlan{
		Intent:        "Незаметно продолжить вмешательство в защитное плетение",
		ImmediateGoal: "Скрыть следы вмешательства и избежать подозрений стражи",
		Transition:    "continue_scene",
	})
	if !plan.IntentFallbackUsed {
		t.Fatal("agency drift did not activate fallback")
	}
	if plan.Intent != action || plan.ImmediateGoal != action {
		t.Fatalf("unsafe planner direction survived fallback: %#v", plan)
	}
	if err := validateTurnPlan(action, plan); err != nil {
		t.Fatalf("fallback plan rejected: %v", err)
	}
}

func TestTurnPlanKeepsNarrativeGoalWhenIntentIsSafe(t *testing.T) {
	action := "Открыть дверь"
	plan := normalizeTurnPlan(action, TurnPlan{
		Intent:        "осторожно открыть дверь",
		ImmediateGoal: "показать сопротивление замка и новую улику",
		Transition:    "continue_scene",
	})
	if plan.IntentFallbackUsed {
		t.Fatal("safe planner direction incorrectly activated fallback")
	}
	if plan.ImmediateGoal != "показать сопротивление замка и новую улику" {
		t.Fatalf("narrative goal was incorrectly replaced: %#v", plan)
	}
	if err := validateTurnPlan(action, plan); err != nil {
		t.Fatalf("safe narrative goal rejected: %v", err)
	}
}
