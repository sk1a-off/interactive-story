package director

import (
	"context"
	"encoding/json"
	"testing"

	d "github.com/local/interactive-ai-story/backend/internal/domain/director"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/directorrepo"
)

func TestHeroAssistPreservesUnrequestedFieldsAndDoesNotApply(t *testing.T) {
	hero := "00000000-0000-4000-8000-000000000001"
	entry := "00000000-0000-4000-8000-000000000002"
	tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000003"))
	repo := &assistRepo{view: directorrepo.View{TimelineID: tid, SemanticRevision: 7,
		Characters: []map[string]any{{"id": hero, "kind": "player", "mood": "calm", "goal": "find home", "active": true}},
		Stats:      []map[string]any{{"ownerId": hero, "ownerType": "character", "key": "health", "valueType": "number", "value": 80, "targetValue": 100, "evolutionMode": "gradual"}},
		Inventory:  []map[string]any{{"id": entry, "category": "item", "name": "Sword", "description": "old", "quantity": 1, "level": "rare", "status": "active", "evidence": "found", "tags": []string{"steel"}}},
	}}
	llm := &assistLLM{output: json.RawMessage(`{"baseRevision":999,"operations":[{"type":"update_character_state","payload":{"mood":"happy"}},{"type":"set_stat","payload":{"key":"health","value":90}},{"type":"upsert_journal_entry","payload":{"entryId":"` + entry + `","description":"sharp"}}]}`)}
	result, err := (Service{Repo: repo, LLM: llm}).Assist(context.Background(), AssistCommand{TimelineID: tid, Section: "hero", Instruction: "Update mood, health and sword description"})
	if err != nil {
		t.Fatal(err)
	}
	if repo.applied != 0 || result.BaseRevision != 7 {
		t.Fatalf("applied=%d revision=%d", repo.applied, result.BaseRevision)
	}
	var state, stat, item, before map[string]any
	json.Unmarshal(result.Operations[0].Payload, &state)
	json.Unmarshal(result.Operations[1].Payload, &stat)
	json.Unmarshal(result.Operations[2].Payload, &item)
	json.Unmarshal(result.Operations[2].Before, &before)
	if state["currentGoal"] != "find home" || state["active"] != true || state["characterId"] != hero {
		t.Fatal(state)
	}
	if stat["targetValue"] != float64(100) || stat["evolutionMode"] != "gradual" || stat["value"] != float64(90) {
		t.Fatal(stat)
	}
	if item["quantity"] != float64(1) || item["level"] != "rare" || item["description"] != "sharp" || before["description"] != "old" {
		t.Fatal(item, before)
	}
}

func TestHeroAssistRejectsUnsafePayloads(t *testing.T) {
	view := directorrepo.View{Characters: []map[string]any{{"id": "00000000-0000-4000-8000-000000000001", "kind": "player"}}}
	for _, tc := range []struct {
		kind    d.ExactCommandType
		payload string
	}{
		{d.UpdateCharacterState, `{"characterId":"other","mood":"happy"}`},
		{d.UpdateCharacterState, `{"mood":null}`},
		{d.SetStat, `{"ownerType":"world","key":"health","value":1}`},
		{d.SetStat, `{"key":"health","value":"1"}`},
		{d.UpsertJournalEntry, `{"entryId":42,"name":"x","category":"ability"}`},
		{d.UpsertJournalEntry, `{"entryId":"unknown","description":"x"}`},
		{d.UpsertJournalEntry, `{"name":"x","category":"item","quantity":1.5}`},
		{d.ArchiveCharacter, `{"characterId":"00000000-0000-4000-8000-000000000001"}`},
	} {
		t.Run(tc.payload, func(t *testing.T) {
			result := AssistResult{Operations: []AssistOperation{{Type: tc.kind, Payload: json.RawMessage(tc.payload)}}}
			if normalizeHeroAssist(&result, view) == nil {
				t.Fatal("unsafe payload accepted")
			}
		})
	}
}
