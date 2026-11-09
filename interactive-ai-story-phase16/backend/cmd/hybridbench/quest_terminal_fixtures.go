package main

import (
	_ "embed"
	"encoding/json"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

//go:embed fixtures/quest-door-replay.json
var doorReplayInput json.RawMessage

func terminalQuestFixtures() []questFixture {
	const parent = "00000000-0000-4000-8000-000000000201"
	const child = "00000000-0000-4000-8000-000000000202"
	var result []questFixture
	for _, tc := range []struct {
		id, beat, criterion string
		children            bool
		operations          map[string]string
	}{
		{"quest-complete", "Алекс закрепил вторую повозку. Обе повозки надёжно закреплены канатами. Работа завершена.", "Обе повозки надёжно закреплены канатами.", false, map[string]string{parent: "complete"}},
		{"quest-fail", "Срок истёк. Письмо не доставлено до заката, адресат погиб. Доставить письмо адресату больше невозможно. Задание окончательно провалено.", "Письмо доставлено адресату до заката.", false, map[string]string{parent: "fail"}},
		{"child-and-parent-complete", "Алекс закрепил вторую повозку. Вторая повозка надёжно закреплена канатами. Обе повозки надёжно закреплены канатами. Работа завершена.", "Обе повозки надёжно закреплены канатами.", true, map[string]string{parent: "complete", child: "complete"}},
	} {
		var input map[string]any
		json.Unmarshal(richQuestFixtures()[0].Input, &input)
		input["action"] = "Завершить текущее действие и проверить фактический результат."
		input["intent"] = input["action"]
		input["newBeat"] = tc.beat
		objectives := []generationtarget.Objective{{ID: parent, Scope: "global", Kind: "quest", QuestType: "main", Title: tc.criterion, SuccessCriteria: tc.criterion, Status: "active", Progress: 50}}
		if tc.children {
			objectives = append(objectives, generationtarget.Objective{ID: child, ParentObjectiveID: parent, Scope: "minor", Kind: "task", QuestType: "main", Title: "Закрепить вторую повозку", SuccessCriteria: "Вторая повозка надёжно закреплена канатами.", Status: "active", Progress: 0})
		}
		input["currentObjectives"] = objectives
		input["context"].(map[string]any)["previousBeatText"] = "Текущий квест ещё активен. Его критерий ещё не выполнен."
		raw, _ := json.Marshal(input)
		result = append(result, questFixture{ID: tc.id, Input: raw, ExpectedOperations: tc.operations})
	}
	result = append(result, questFixture{ID: "door-frozen-final-state", Input: doorReplayInput, ExpectedOperations: map[string]string{parent: "complete"}})
	var negative map[string]any
	json.Unmarshal(doorReplayInput, &negative)
	negative["newBeat"] = "Алекс представил, что дверь полностью открыта. В действительности она осталась закрытой: ручку заклинило, и открыть дверь не удалось."
	raw, _ := json.Marshal(negative)
	result = append(result, questFixture{ID: "door-imagined-not-open", Input: raw})
	return result
}
