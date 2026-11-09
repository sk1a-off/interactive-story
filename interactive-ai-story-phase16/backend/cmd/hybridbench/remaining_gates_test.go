package main

import (
	"encoding/json"
	"testing"
)

func TestRemainingNegativeGateRejectsFalseProgress(t *testing.T) {
	set, err := loadCaseSet("fixtures/remaining-gates-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Cases) != 5 {
		t.Fatal("missing scenarios")
	}
	for _, item := range set.Cases {
		if item.Target == nil {
			t.Fatal("live target dependency")
		}
	}
	for _, item := range set.Cases[:3] {
		base := item.Target.Objectives[0]
		if !objectiveProgressUnchanged(nil, item.Target.Objectives) {
			t.Fatal("unchanged state rejected")
		}
		for _, tc := range []struct {
			progress          int
			status, criterion string
			want              bool
		}{
			{base.Progress, "active", base.SuccessCriteria, true},
			{base.Progress + 1, "active", base.SuccessCriteria, false},
			{100, "completed", base.SuccessCriteria, false},
			{base.Progress, "active", "Подменённый критерий", false},
		} {
			raw, _ := json.Marshal(map[string]any{"objectiveId": base.ID, "scope": base.Scope, "progress": tc.progress, "status": tc.status, "successCriteria": tc.criterion})
			if objectiveProgressUnchanged([]eventResult{{Type: "objective_updated", Payload: raw}}, item.Target.Objectives) != tc.want {
				t.Fatalf("%s: %+v", item.ID, tc)
			}
		}
	}
}
