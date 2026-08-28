package postgres_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	pgadapter "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	appdirector "github.com/local/interactive-ai-story/backend/internal/application/director"
	appsaves "github.com/local/interactive-ai-story/backend/internal/application/saves"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	domaindir "github.com/local/interactive-ai-story/backend/internal/domain/director"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/savepoint"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/setuprepo"
)

func TestDirectorWorldEntitiesAreEditableArchivableAndReachGenerationContext(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	pool, err := bootstrap.OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ownerID, _ := id.New()
	storyID, _ := id.New()
	timelineID, _ := id.New()
	_, err = pool.Exec(ctx, `INSERT INTO users(id,username) VALUES($1,$2)`, ownerID, "director-world-"+ownerID.String())
	if err != nil {
		t.Fatal(err)
	}
	created, _ := story.New(story.ID(storyID), story.UserID(ownerID), "Director world", time.Now().UTC())
	repo := pgadapter.NewSetupRepository(pool)
	if err = repo.CreateStory(ctx, created, nil); err != nil {
		t.Fatal(err)
	}
	tl, _ := timeline.New(timeline.ID(timelineID), story.ID(storyID), "Main")
	chapterID, _ := id.New()
	sceneID, _ := id.New()
	beatID, _ := id.New()
	material := setuprepo.StartMaterialization{ChapterID: chapterID, SceneID: sceneID, BeatID: beatID, ChapterTitle: "Начало", ChapterGoal: "Найти путь", SceneGoal: "Войти в архив", OpeningText: "Архив открылся.", Choices: []string{"A", "B", "C", "D"}, Characters: []setuprepo.InitialCharacter{{Kind: "player", Name: "Лада", Age: 24}, {Kind: "persistent_npc", Name: "Тихон", Age: 61, Role: "архивариус"}}, Locations: []setuprepo.InitialLocation{{Name: "Архив", Description: "Затопленное хранилище"}}, Quests: []setuprepo.InitialQuest{{QuestType: "main", Title: "Найти Мирона", SuccessCriteria: "Мирон найден", Stages: []setuprepo.InitialQuestStage{{Kind: "task", Title: "Прочитать карту", SuccessCriteria: "Маршрут получен"}}}}}
	if err = repo.StartStory(ctx, story.ID(storyID), tl, material); err != nil {
		t.Fatal(err)
	}
	directorRepo := pgadapter.NewDirectorRepository(pool)
	saveService := appsaves.NewService(pgadapter.NewUnitOfWork(pool), time.Now, nil)
	service := appdirector.Service{Repo: directorRepo, Safety: func(ctx context.Context, tid timeline.ID, note string) (id.ID, error) {
		saved, saveErr := saveService.Create(ctx, appsaves.CreateCommand{TimelineID: tid, Name: "Director integration safety", Note: note, Kind: savepoint.KindSystem})
		return id.ID(saved.ID), saveErr
	}}
	view, err := service.View(ctx, timeline.ID(timelineID))
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Characters) != 2 || len(view.Locations) != 1 {
		t.Fatalf("setup world was not materialized: characters=%d locations=%d", len(view.Characters), len(view.Locations))
	}
	apply := func(rev int64, kind domaindir.ExactCommandType, payload string) {
		t.Helper()
		_, applyErr := service.ApplyExact(ctx, domaindir.ExactCommand{TimelineID: timeline.ID(timelineID), ExpectedRevision: rev, Type: kind, Payload: json.RawMessage(payload)})
		if applyErr != nil {
			t.Fatal(applyErr)
		}
	}
	newCharacter, _ := id.New()
	apply(view.SemanticRevision, domaindir.UpsertCharacter, `{"characterId":"`+newCharacter.String()+`","name":"Марта","age":34,"kind":"persistent_npc","role":"проводник","personality":"осторожная","relationship":"не доверяет Ладе","visualAnchorEn":"adult woman in green coat","mood":"насторожена","currentGoal":"провести к шлюзу","active":true}`)
	newLocation, _ := id.New()
	apply(view.SemanticRevision+1, domaindir.UpsertLocation, `{"locationId":"`+newLocation.String()+`","name":"Северный шлюз","description":"Закрытый приливной проход","visualAnchorEn":"ancient flooded stone gate","active":true}`)
	questID, _ := id.New()
	apply(view.SemanticRevision+2, domaindir.UpsertObjective, `{"objectiveId":"`+questID.String()+`","scope":"global","kind":"quest","questType":"side","title":"Помочь Марте","description":"Вернуть журнал","successCriteria":"Журнал возвращён Марте","status":"active","progress":0}`)
	stageID, _ := id.New()
	apply(view.SemanticRevision+3, domaindir.UpsertObjective, `{"objectiveId":"`+stageID.String()+`","parentObjectiveId":"`+questID.String()+`","scope":"minor","kind":"task","questType":"side","title":"Найти журнал","successCriteria":"Журнал получен","status":"active","progress":0}`)
	view, err = service.View(ctx, timeline.ID(timelineID))
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Characters) != 3 || len(view.Locations) != 2 || len(view.Objectives) != 4 {
		t.Fatalf("created entities missing: characters=%d locations=%d objectives=%d", len(view.Characters), len(view.Locations), len(view.Objectives))
	}
	var heroID string
	for _, character := range view.Characters {
		if character["kind"] == "player" {
			heroID, _ = character["id"].(string)
		}
	}
	if heroID == "" {
		t.Fatal("player character is missing from director view")
	}
	instruction, err := service.AddInstruction(ctx, domaindir.InstructionCommand{
		TimelineID: timeline.ID(timelineID), Text: "Сначала вести к архиву", Scope: narrative.ScopeNextBeat, Priority: narrative.PriorityNormal,
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err = service.View(ctx, timeline.ID(timelineID))
	if err != nil {
		t.Fatal(err)
	}
	journalID, _ := id.New()
	apply(view.SemanticRevision, domaindir.UpsertJournalEntry, `{"entryId":"`+journalID.String()+`","category":"ability","name":"Зрение прилива","description":"Видит направление подводных потоков","level":"уверенный","status":"active","evidence":"Исправлено режиссёром","tags":["вода"]}`)
	apply(view.SemanticRevision+1, domaindir.UpdateInstruction, `{"instructionId":"`+instruction.ID.String()+`","text":"Сначала вести к северному шлюзу","scope":"persistent","priority":"high","status":"active"}`)
	apply(view.SemanticRevision+2, domaindir.UpdateCharacterState, `{"CharacterID":"`+heroID+`","Mood":"сосредоточена","CurrentGoal":"открыть северный шлюз","Active":true}`)
	view, err = service.View(ctx, timeline.ID(timelineID))
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Abilities) != 1 || view.Abilities[0]["name"] != "Зрение прилива" || view.Abilities[0]["category"] != "ability" {
		t.Fatalf("hero journal correction was not returned by director view: %#v", view.Abilities)
	}
	if len(view.Instructions) != 1 || view.Instructions[0].Text != "Сначала вести к северному шлюзу" || view.Instructions[0].Scope != narrative.ScopePersistent || view.Instructions[0].Priority != narrative.PriorityHigh {
		t.Fatalf("applied instruction was not editable/reactivated: %#v", view.Instructions)
	}
	var correctedHero map[string]any
	for _, character := range view.Characters {
		if character["id"] == heroID {
			correctedHero = character
		}
	}
	if correctedHero["mood"] != "сосредоточена" || correctedHero["goal"] != "открыть северный шлюз" {
		t.Fatalf("hero state correction was not persisted: %#v", correctedHero)
	}
	apply(view.SemanticRevision, domaindir.ArchiveCharacter, `{"characterId":"`+newCharacter.String()+`"}`)
	apply(view.SemanticRevision+1, domaindir.ArchiveLocation, `{"locationId":"`+newLocation.String()+`"}`)
	apply(view.SemanticRevision+2, domaindir.ArchiveObjective, `{"objectiveId":"`+questID.String()+`"}`)
	target, err := pgadapter.NewGenerationTarget(pool).CurrentWriteTarget(ctx, timeline.ID(timelineID))
	if err != nil {
		t.Fatal(err)
	}
	if string(target.InitialCast) == "" || string(target.World) == "" {
		t.Fatal("live world did not reach generation target")
	}
	if containsJSON(target.InitialCast, "Марта") || containsJSON(target.World, "Северный шлюз") {
		t.Fatalf("archived entities leaked into generation context: cast=%s world=%s", target.InitialCast, target.World)
	}
	view, _ = service.View(ctx, timeline.ID(timelineID))
	for _, objective := range view.Objectives {
		if objective["id"] == questID.String() || objective["parentObjectiveId"] == questID.String() {
			if objective["status"] != "failed" {
				t.Fatalf("archived quest hierarchy stayed active: %#v", objective)
			}
		}
	}
}

func containsJSON(raw json.RawMessage, needle string) bool {
	return len(raw) > 0 && string(raw) != "null" && containsString(string(raw), needle)
}
func containsString(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
