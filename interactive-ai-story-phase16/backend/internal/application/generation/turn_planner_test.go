package generation

import "testing"

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
