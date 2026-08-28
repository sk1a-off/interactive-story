package postgres_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	pgadapter "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/application/canon"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

func TestLifecycleTransitionSelectsNewestChapterAndExpiresInstruction(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := bootstrap.OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	userID, _ := id.New()
	storyID, _ := id.New()
	timelineID, _ := id.New()
	chapterOne, _ := id.New()
	sceneOne, _ := id.New()
	beatOne, _ := id.New()
	instructionID, _ := id.New()
	if _, err = pool.Exec(ctx, `INSERT INTO users(id,username) VALUES($1,$2)`, userID, "lifecycle-"+userID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO stories(id,owner_id,title,status) VALUES($1,$2,'Lifecycle integration','active')`, storyID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO timelines(id,story_id,name,status) VALUES($1,$2,'Main','active')`, timelineID, storyID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO chapters(id,timeline_id,number,title,goal,status) VALUES($1,$2,1,'Старая глава','Закончить спор','active')`, chapterOne, timelineID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO scenes(id,chapter_id,number,goal,status) VALUES($1,$2,7,'Уйти из таверны','awaiting_player')`, sceneOne, chapterOne); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO beats(id,scene_id,position,kind,content,status,is_active) VALUES($1,$2,20,'mixed','{"text":"Старый финал сцены"}','committed',true)`, beatOne, sceneOne); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO director_instructions(id,timeline_id,instruction_text,scope,priority,status,created_at_seq) VALUES($1,$2,'Только следующий ход','next_beat','normal','active',0)`, instructionID, timelineID); err != nil {
		t.Fatal(err)
	}

	chapterTwo, _ := id.New()
	sceneTwo, _ := id.New()
	beatTwo, _ := id.New()
	characterID, _ := id.New()
	locationID, _ := id.New()
	pending := []event.PendingEvent{
		{Type: "scene_completed", SchemaVersion: 1, Payload: lifecycleJSON(map[string]any{"sceneId": sceneOne, "status": "completed"})},
		{Type: "chapter_completed", SchemaVersion: 1, Payload: lifecycleJSON(map[string]any{"chapterId": chapterOne, "status": "completed"})},
		{Type: "chapter_created", SchemaVersion: 1, Payload: lifecycleJSON(map[string]any{"chapterId": chapterTwo, "number": 2, "title": "За стенами", "goal": "Найти новый путь", "status": "active"})},
		{Type: "scene_started", SchemaVersion: 1, Payload: lifecycleJSON(map[string]any{"sceneId": sceneTwo, "chapterId": chapterTwo, "number": 1, "goal": "Осмотреть дорогу", "status": "awaiting_player"})},
		{Type: "beat_committed", SchemaVersion: 1, Payload: lifecycleJSON(map[string]any{"beatId": beatTwo, "sceneId": sceneTwo, "position": 1, "kind": "transition", "text": "Утром герой вышел за городские ворота.", "status": "committed"})},
		{Type: "character_world_upserted", SchemaVersion: 1, Payload: lifecycleJSON(map[string]any{"characterId": characterID, "name": "Смотритель Илья", "age": 43, "kind": "persistent_npc", "role": "хранитель", "mood": "насторожен", "currentGoal": "закрыть архив", "active": true})},
		{Type: "location_world_upserted", SchemaVersion: 1, Payload: lifecycleJSON(map[string]any{"locationId": locationID, "name": "Закрытый архив", "description": "Подземное хранилище", "visualAnchorEn": "underground archive vault", "active": true})},
		{Type: "director_instructions_expired", SchemaVersion: 1, Payload: lifecycleJSON(map[string]any{"instructionIds": []id.ID{instructionID}})},
		{Type: "choices_ready", SchemaVersion: 1, Payload: lifecycleJSON(map[string]any{"beatId": beatTwo, "choices": []string{"A", "B", "C", "D"}})},
	}
	service := canon.NewService(pgadapter.NewUnitOfWork(pool), time.Now)
	committed, err := service.AppendSemantic(ctx, timeline.ID(timelineID), 0, pending)
	if err != nil {
		t.Fatal(err)
	}

	var oldChapterStatus, oldSceneStatus, instructionStatus string
	var chapterEnded, sceneEnded bool
	if err = pool.QueryRow(ctx, `SELECT status,ended_at IS NOT NULL FROM chapters WHERE id=$1`, chapterOne).Scan(&oldChapterStatus, &chapterEnded); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT status,ended_at IS NOT NULL FROM scenes WHERE id=$1`, sceneOne).Scan(&oldSceneStatus, &sceneEnded); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM director_instructions WHERE id=$1`, instructionID).Scan(&instructionStatus); err != nil {
		t.Fatal(err)
	}
	if oldChapterStatus != "completed" || oldSceneStatus != "completed" || !chapterEnded || !sceneEnded || instructionStatus != "expired" {
		t.Fatalf("old lifecycle not closed: chapter=%s/%v scene=%s/%v instruction=%s", oldChapterStatus, chapterEnded, oldSceneStatus, sceneEnded, instructionStatus)
	}
	if _, err = service.AppendSemantic(ctx, timeline.ID(timelineID), int64(len(committed)), []event.PendingEvent{
		{Type: "character_world_upserted", SchemaVersion: 1, Payload: lifecycleJSON(map[string]any{"characterId": characterID, "name": "Илья из северного архива", "age": 43, "kind": "persistent_npc", "role": "хранитель", "mood": "спокоен", "currentGoal": "помочь герою", "active": true})},
		{Type: "location_world_upserted", SchemaVersion: 1, Payload: lifecycleJSON(map[string]any{"locationId": locationID, "name": "Северный архив", "description": "Открытое подземное хранилище", "visualAnchorEn": "open underground archive vault", "active": true})},
	}); err != nil {
		t.Fatal(err)
	}
	var baseCharacterName, baseLocationName string
	if err = pool.QueryRow(ctx, `SELECT name FROM characters WHERE id=$1`, characterID).Scan(&baseCharacterName); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT name FROM locations WHERE id=$1`, locationID).Scan(&baseLocationName); err != nil {
		t.Fatal(err)
	}
	if baseCharacterName != "Смотритель Илья" || baseLocationName != "Закрытый архив" {
		t.Fatalf("branch-local world update leaked into story-global identity: character=%q location=%q", baseCharacterName, baseLocationName)
	}

	target, err := pgadapter.NewGenerationTarget(pool).CurrentWriteTarget(ctx, timeline.ID(timelineID))
	if err != nil {
		t.Fatal(err)
	}
	if target.ChapterID != chapterTwo || target.SceneID != sceneTwo || target.ChapterNumber != 2 || target.SceneNumber != 1 || target.NextBeatPosition != 2 || len(target.RecentBeats) != 2 {
		t.Fatalf("wrong write target after transition: %#v", target)
	}
	var liveCast struct {
		Characters []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"characters"`
	}
	var liveWorld struct {
		Locations []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"locations"`
	}
	if json.Unmarshal(target.InitialCast, &liveCast) != nil || len(liveCast.Characters) != 1 || liveCast.Characters[0].ID != characterID.String() || liveCast.Characters[0].Name != "Илья из северного архива" {
		t.Fatalf("generated character missing from next generation context: %s", target.InitialCast)
	}
	if json.Unmarshal(target.World, &liveWorld) != nil || len(liveWorld.Locations) != 1 || liveWorld.Locations[0].ID != locationID.String() || liveWorld.Locations[0].Name != "Северный архив" {
		t.Fatalf("generated location missing from next generation context: %s", target.World)
	}
	current, err := pgadapter.NewReaderRepository(pool).Current(ctx, timeline.ID(timelineID))
	if err != nil {
		t.Fatal(err)
	}
	if current.ChapterTitle != "За стенами" || current.SceneID != sceneTwo || current.BeatID != beatTwo || current.Text != "Утром герой вышел за городские ворота." || current.ChoiceSet == nil || current.ChoiceSet.BeatID != beatTwo {
		t.Fatalf("reader did not move to newest chapter: %#v", current)
	}
	legacyInstructionID, _ := id.New()
	if _, err = pool.Exec(ctx, `INSERT INTO director_instructions(id,timeline_id,instruction_text,scope,priority,status,created_at_seq) VALUES($1,$2,'Устаревшее указание','next_beat','normal','active',0)`, legacyInstructionID, timelineID); err != nil {
		t.Fatal(err)
	}
	activeInstructions, err := pgadapter.NewDirectorRepository(pool).ActiveInstructions(ctx, timeline.ID(timelineID))
	if err != nil {
		t.Fatal(err)
	}
	if len(activeInstructions) != 0 {
		t.Fatalf("legacy next-beat instruction remained active: %#v", activeInstructions)
	}
}

func lifecycleJSON(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}
