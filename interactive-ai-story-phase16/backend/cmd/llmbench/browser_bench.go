package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

func browserRoleSuite() []testCase {
	out := suite()
	for i, tc := range out {
		if tc.Name == "state_evaluator" {
			out[i].Validate = browserJournalValidation(tc)
		}
	}
	// A larger frozen context: input transport/retention, not live story history.
	for _, tc := range suite() {
		if tc.Name == "writer_continuation" {
			raw, _ := json.Marshal(tc.Input)
			var input map[string]any
			_ = json.Unmarshal(raw, &input)
			context := input["context"].(map[string]any)
			var facts []string
			for i := 0; i < 85; i++ {
				facts = append(facts, fmt.Sprintf("Архивная запись %03d: в городе Велесье после паводка перекрыт старый водовод; дежурные ведут журнал уровня воды. Это справочная история города, а не текущее действие Лады.", i))
			}
			context["backgroundFacts"] = facts
			tc.Name = "writer_large_context"
			tc.Input = input
			out = append(out, tc)
		}
	}
	action := "Осторожно осмотреть запертую дверь архива и её петли, не пытаясь вскрыть замок."
	target := generationtarget.Target{PreviousBeatText: "Мира остановилась перед запертой дверью архива.", SceneGoal: "Найти проход к башне", NextBeatPosition: 4, ChapterBeatCount: 3}
	out = append(out, testCase{Name: "turn_planner_browser", Role: "turn_planner", MaxTokens: 1600, Temperature: .3, TopP: .9,
		Input: map[string]any{"action": action, "directorInstructions": []any{}, "heroJournal": []any{}, "quests": []any{}, "context": map[string]any{"previousBeatText": target.PreviousBeatText, "sceneGoal": target.SceneGoal, "nextBeatPosition": 4, "chapterBeatCount": 3}},
		Validate: func(_ map[string]any, raw string) validation {
			m := fakeai.NewScriptedStoryLLM(map[string][]byte{"turn_planner": []byte(raw)})
			p, err := (appgen.LLMTurnPlanner{LLM: m}).Plan(context.Background(), appgen.PlayerAction{Text: action}, target, nil)
			ok := err == nil && p.Transition == "continue_scene" && !p.IntentFallbackUsed
			v := validation{SchemaScore: boolScore(err == nil), InstructionScore: boolScore(ok), LanguageScore: russianRatio(p.Intent), ContinuityScore: boolScore(ok)}
			if err != nil {
				v.Notes = []string{err.Error()}
			}
			return v
		}})
	const place = "00000000-0000-4000-8000-000000000302"
	world := json.RawMessage(`{"locations":[{"locationId":"` + place + `","name":"Архив","description":"Каменный архив с дубовой дверью","visualAnchorEn":"Stone archive with an oak door"}]}`)
	for _, f := range []struct {
		name, beat string
		changes    int
	}{
		{"world_browser_positive", "Молния ударила в Архив. На его каменном фасаде появилась свежая глубокая трещина, но дубовая дверь уцелела. Трещина не исчезла: здание действительно повреждено и требует ремонта.", 1},
		{"world_browser_negative", "Лада вообразила, как Архив сгорел. Это была лишь тревожная фантазия. В действительности каменный Архив с дубовой дверью стоял целым и неизменным.", 0},
	} {
		f := f
		out = append(out, testCase{Name: f.name, Role: "world_evaluator", MaxTokens: 1400, Temperature: .25, TopP: .8,
			Input: map[string]any{"newBeat": f.beat, "action": "Осмотреть Архив", "intent": "Осмотреть Архив", "context": map[string]any{"world": world, "player": map[string]string{"name": "Лада"}}},
			Validate: func(_ map[string]any, raw string) validation {
				normalized, err := appgen.CheckWorldProposal(f.beat, generationtarget.Target{World: world}, json.RawMessage(raw))
				var changes []struct {
					Type       string `json:"type"`
					LocationID string `json:"locationId"`
				}
				parseErr := json.Unmarshal(normalized, &changes)
				ok := err == nil && parseErr == nil && len(changes) == f.changes
				if f.changes == 1 && ok {
					ok = changes[0].Type == "upsert_location" && changes[0].LocationID == place
				}

				v := validation{SchemaScore: boolScore(err == nil), InstructionScore: boolScore(ok), LanguageScore: 1, ContinuityScore: boolScore(ok)}
				if err != nil {
					v.Notes = []string{err.Error()}
				} else if !ok {
					v.Notes = []string{"unexpected durable changes or location identity"}
				}
				return v
			}})
	}
	// Transport escaping: output must preserve quotes, backslashes and real newlines.
	sample := "Мира сказала: \"Путь C:\\archive закрыт\".\n\nЛада сохранила строку без изменений."
	out = append(out, testCase{Name: "browser_escaping", Role: "writer", MaxTokens: 500, Input: map[string]any{"instruction": "For this transport test, return exactly {text: payload} as valid JSON; preserve every character. Do not continue the story.", "payload": sample},
		Contract: "Return exactly one JSON object with the field text copied byte-for-byte from the input payload. This is an escaping transport test.",
		Validate: func(obj map[string]any, _ string) validation {
			ok := len(obj) == 1 && stringField(obj, "text") == sample
			return validation{SchemaScore: boolScore(ok), InstructionScore: boolScore(ok), LanguageScore: 1, ContinuityScore: boolScore(ok)}
		}})
	return out
}

func browserContract(tc testCase, prompts map[string]string) (testCase, error) {
	if tc.Name == "browser_escaping" {
		return tc, nil
	}
	if strings.TrimSpace(prompts[tc.Role]) == "" {
		return tc, fmt.Errorf("missing active contract for %s", tc.Role)
	}
	tc.Contract = prompts[tc.Role]
	return tc, nil
}

// The active evidenceQuote contract is validated through production gates.
// Legacy benchmark semantics (balances/items) are retained after field adaptation.
func browserJournalValidation(tc testCase) func(map[string]any, string) validation {
	input := tc.Input.(map[string]any)
	beat := input["newBeat"].(string)
	data, _ := json.Marshal(input["currentJournal"])
	var journal []generationtarget.JournalEntry
	_ = json.Unmarshal(data, &journal)
	return func(_ map[string]any, raw string) validation {
		_, err := appgen.CheckJournalProposal(beat, journal, json.RawMessage(raw))
		var adapted map[string]any
		_ = json.Unmarshal([]byte(raw), &adapted)
		changes, _ := adapted["changes"].([]any)
		for _, value := range changes {
			if c, ok := value.(map[string]any); ok {
				if quote, exists := c["evidenceQuote"]; exists {
					c["evidence"] = quote
				}
			}
		}
		v := validateJournalChanges(adapted, raw)
		v.SchemaScore = boolScore(err == nil)
		if err != nil {
			v.Notes = append(v.Notes, "production journal gate: "+err.Error())
		}
		return v
	}
}
