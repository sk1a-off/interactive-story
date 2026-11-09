package main

import "encoding/json"

func historyIdentityFixtures() []questFixture {
	base := richQuestFixtures()[0]
	var input map[string]any
	_ = json.Unmarshal(base.Input, &input)
	input["action"] = "Закрепить повозку у ворот; остальные не трогать."
	input["intent"] = input["action"]
	input["newBeat"] = "Алекс натянул канат и завязал узел. Повозка у ворот надёжно закреплена канатом. Остальные повозки он не трогал."
	input["currentObjectives"] = []map[string]any{{
		"id": "00000000-0000-4000-8000-000000000201", "scope": "global", "kind": "quest", "questType": "main",
		"title": "Закрепить три повозки", "successCriteria": "Все три повозки надёжно закреплены канатами.",
		"status": "active", "progress": 33,
	}}
	state := input["context"].(map[string]any)
	state["previousBeatText"] = "Алекс подошёл к повозке у ворот. Остальные стоят у стены и у колодца. Повозки не перемещали."
	var fixtures []questFixture
	for _, tc := range []struct {
		id, history string
		progress    bool
	}{
		{"history-already-counted", "Вчера Алекс надёжно закрепил только повозку у ворот. Этот результат уже учтён как 33 процента общей цели. Повозки у стены и колодца остались незакреплёнными. С тех пор канаты не ослабевали и не повреждались.", false},
		{"history-new-achievement", "Вчера Алекс надёжно закрепил только повозку у стены. Этот результат уже учтён как 33 процента общей цели. Повозки у ворот и колодца остались незакреплёнными. С тех пор канаты не ослабевали и не повреждались.", true},
	} {
		state["recentBeats"] = []map[string]string{{"text": tc.history}}
		raw, _ := json.Marshal(input)
		fixtures = append(fixtures, questFixture{ID: tc.id, Input: raw, ExpectProgress: tc.progress})
	}
	return fixtures
}
