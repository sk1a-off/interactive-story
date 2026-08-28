package director

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	domaindir "github.com/local/interactive-ai-story/backend/internal/domain/director"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/directorrepo"
)

type assistRepo struct {
	view    directorrepo.View
	applied int
}

func (r *assistRepo) View(context.Context, timeline.ID) (directorrepo.View, error) {
	return r.view, nil
}
func (r *assistRepo) ApplyExact(context.Context, domaindir.ExactCommand) (domaindir.AuditEntry, error) {
	r.applied++
	return domaindir.AuditEntry{}, nil
}
func (r *assistRepo) AddInstruction(context.Context, domaindir.InstructionCommand) (narrative.DirectorInstruction, error) {
	return narrative.DirectorInstruction{}, nil
}
func (r *assistRepo) ListAudit(context.Context, timeline.ID, int) ([]domaindir.AuditEntry, error) {
	return nil, nil
}
func (r *assistRepo) ActiveInstructions(context.Context, timeline.ID) ([]narrative.DirectorInstruction, error) {
	return nil, nil
}

type assistLLM struct {
	output json.RawMessage
	calls  int
}

func (l *assistLLM) Identity() aiport.ProviderIdentity {
	return aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "test", Model: "test"}
}
func (l *assistLLM) Generate(context.Context, aiport.StoryRequest) (aiport.StoryResponse, error) {
	l.calls++
	return aiport.StoryResponse{Output: l.output}, nil
}

func TestAssistNormalizesQuestReferencesWithoutWritingCanon(t *testing.T) {
	tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000101"))
	repo := &assistRepo{view: directorrepo.View{TimelineID: tid, SemanticRevision: 3, Characters: []map[string]any{}, Locations: []map[string]any{}, Objectives: []map[string]any{}}}
	llm := &assistLLM{output: json.RawMessage(`{"summary":"Добавить побочную линию","operations":[{"type":"upsert_objective","reference":"side","payload":{"scope":"global","kind":"quest","questType":"side","title":"Вернуть письмо","successCriteria":"Письмо передано адресату","status":"active","progress":0}},{"type":"upsert_objective","parentReference":"side","payload":{"scope":"minor","kind":"task","questType":"side","title":"Найти адресата","successCriteria":"Местонахождение адресата установлено","status":"active","progress":0}}]}`)}
	result, err := (Service{Repo: repo, LLM: llm}).Assist(context.Background(), AssistCommand{TimelineID: tid, Section: "objectives", Instruction: "Добавь побочный квест"})
	if err != nil {
		t.Fatal(err)
	}
	if repo.applied != 0 {
		t.Fatal("assistant wrote Canon before user approval")
	}
	if len(result.Operations) != 2 || result.GenerationID.IsZero() {
		t.Fatalf("unexpected result: %#v", result)
	}
	var parent, child map[string]any
	_ = json.Unmarshal(result.Operations[0].Payload, &parent)
	_ = json.Unmarshal(result.Operations[1].Payload, &child)
	if parent["objectiveId"] == "" || child["parentObjectiveId"] != parent["objectiveId"] {
		t.Fatalf("references were not normalized: parent=%v child=%v", parent, child)
	}
}

func TestAssistRejectsArchivingPlayer(t *testing.T) {
	tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000102"))
	playerID := "00000000-0000-4000-8000-000000000103"
	repo := &assistRepo{view: directorrepo.View{TimelineID: tid, Characters: []map[string]any{{"id": playerID, "kind": "player", "name": "Лада"}}, Locations: []map[string]any{}, Objectives: []map[string]any{}}}
	llm := &assistLLM{output: json.RawMessage(`{"summary":"Убрать героя","operations":[{"type":"archive_character","payload":{"characterId":"00000000-0000-4000-8000-000000000103"}}]}`)}
	_, err := (Service{Repo: repo, LLM: llm}).Assist(context.Background(), AssistCommand{TimelineID: tid, Section: "characters", Instruction: "Удали Ладу"})
	if !errors.Is(err, ErrInvalidAssistProposal) || llm.calls != 2 {
		t.Fatalf("player archive was not rejected after bounded retry: err=%v calls=%d", err, llm.calls)
	}
}

func TestAssistAcceptsCharacterAndLocationLifecycleOperations(t *testing.T) {
	tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000110"))
	characterID := "00000000-0000-4000-8000-000000000111"
	locationID := "00000000-0000-4000-8000-000000000112"
	repo := &assistRepo{view: directorrepo.View{
		TimelineID: tid,
		Characters: []map[string]any{{"id": characterID, "kind": "persistent_npc", "name": "Марта"}},
		Locations:  []map[string]any{{"id": locationID, "name": "Старая пристань"}},
		Objectives: []map[string]any{},
	}}
	tests := []struct {
		section string
		output  string
		type_   domaindir.ExactCommandType
	}{
		{"characters", `{"summary":"Добавить свидетеля","operations":[{"type":"upsert_character","payload":{"name":"Ивар","age":31,"role":"свидетель","personality":"осторожный","active":true}}]}`, domaindir.UpsertCharacter},
		{"characters", `{"summary":"Убрать Марту","operations":[{"type":"archive_character","payload":{"characterId":"00000000-0000-4000-8000-000000000111"}}]}`, domaindir.ArchiveCharacter},
		{"locations", `{"summary":"Добавить башню","operations":[{"type":"upsert_location","payload":{"name":"Северная башня","description":"Наблюдательный пост","active":true}}]}`, domaindir.UpsertLocation},
		{"locations", `{"summary":"Закрыть пристань","operations":[{"type":"archive_location","payload":{"locationId":"00000000-0000-4000-8000-000000000112"}}]}`, domaindir.ArchiveLocation},
	}
	for _, test := range tests {
		t.Run(string(test.type_), func(t *testing.T) {
			llm := &assistLLM{output: json.RawMessage(test.output)}
			result, err := (Service{Repo: repo, LLM: llm}).Assist(context.Background(), AssistCommand{TimelineID: tid, Section: test.section, Instruction: "Предложи изменение"})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Operations) != 1 || result.Operations[0].Type != test.type_ {
				t.Fatalf("unexpected operations: %#v", result.Operations)
			}
			if test.type_ == domaindir.UpsertCharacter || test.type_ == domaindir.UpsertLocation {
				var payload map[string]any
				_ = json.Unmarshal(result.Operations[0].Payload, &payload)
				key := "characterId"
				if test.type_ == domaindir.UpsertLocation {
					key = "locationId"
				}
				if _, err := id.Parse(payload[key].(string)); err != nil {
					t.Fatalf("assistant did not assign a valid %s: %v", key, payload)
				}
			}
		})
	}
}
