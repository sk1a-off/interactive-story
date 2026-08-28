package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	pgadapter "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/setuprepo"
)

func TestStartedStoryMaterializesGlobalAndMinorObjectives(t *testing.T) {
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

	ownerID, _ := id.New()
	storyID, _ := id.New()
	timelineID, _ := id.New()
	if _, err = pool.Exec(ctx, `INSERT INTO users(id,username) VALUES($1,$2)`, ownerID, "objectives-"+ownerID.String()); err != nil {
		t.Fatal(err)
	}
	created, err := story.New(story.ID(storyID), story.UserID(ownerID), "Objective integration", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	repo := pgadapter.NewSetupRepository(pool)
	if err = repo.CreateStory(ctx, created, nil); err != nil {
		t.Fatal(err)
	}
	tl, err := timeline.New(timeline.ID(timelineID), story.ID(storyID), "Main")
	if err != nil {
		t.Fatal(err)
	}
	chapterID, _ := id.New()
	sceneID, _ := id.New()
	beatID, _ := id.New()
	materialization := setuprepo.StartMaterialization{ChapterID: chapterID, SceneID: sceneID, BeatID: beatID, ChapterTitle: "Opening", ChapterGoal: "Stop the storm", SceneGoal: "Reach the lighthouse", OpeningText: "The storm rises.", Choices: []string{"Run", "Signal", "Hide", "Wait"}, Quests: []setuprepo.InitialQuest{
		{Title: "Stop the storm", SuccessCriteria: "The storm is stopped", Stages: []setuprepo.InitialQuestStage{{Kind: "task", Title: "Reach the lighthouse", SuccessCriteria: "The lighthouse is reached"}, {Kind: "milestone", Title: "Restart the beacon", SuccessCriteria: "The beacon shines"}}},
		{Title: "Find the missing crew", SuccessCriteria: "The crew is found", Stages: []setuprepo.InitialQuestStage{{Kind: "event", Title: "Receive the distress signal", SuccessCriteria: "The signal is decoded"}}},
	}}
	if err = repo.StartStory(ctx, story.ID(storyID), tl, materialization); err != nil {
		t.Fatal(err)
	}

	var head, objectiveRows, objectiveEvents int
	if err = pool.QueryRow(ctx, `SELECT head_event_seq FROM timelines WHERE id=$1`, timelineID).Scan(&head); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM story_threads WHERE timeline_id=$1 AND metadata->>'kind'='objective'`, timelineID).Scan(&objectiveRows); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM story_events WHERE timeline_id=$1 AND event_type='objective_created'`, timelineID).Scan(&objectiveEvents); err != nil {
		t.Fatal(err)
	}
	if head != 9 || objectiveRows != 5 || objectiveEvents != 5 {
		t.Fatalf("head=%d rows=%d events=%d", head, objectiveRows, objectiveEvents)
	}

	current, err := pgadapter.NewReaderRepository(pool).Current(ctx, timeline.ID(timelineID))
	if err != nil {
		t.Fatal(err)
	}
	byTitle := make(map[string]int, len(current.Objectives))
	for index, objective := range current.Objectives {
		byTitle[objective.Title] = index
	}
	storm := current.Objectives[byTitle["Stop the storm"]]
	crew := current.Objectives[byTitle["Find the missing crew"]]
	reach := current.Objectives[byTitle["Reach the lighthouse"]]
	beacon := current.Objectives[byTitle["Restart the beacon"]]
	signal := current.Objectives[byTitle["Receive the distress signal"]]
	if len(current.Objectives) != 5 || storm.Scope != "global" || storm.Kind != "quest" || crew.Scope != "global" || crew.Kind != "quest" || reach.Kind != "task" || reach.ParentObjectiveID != storm.ID || beacon.Kind != "milestone" || beacon.ParentObjectiveID != storm.ID || signal.Kind != "event" || signal.ParentObjectiveID != crew.ID {
		t.Fatalf("unexpected reader objectives: %+v", current.Objectives)
	}
	director, err := pgadapter.NewDirectorRepository(pool).View(ctx, timeline.ID(timelineID))
	if err != nil {
		t.Fatal(err)
	}
	directorParents := make(map[string]any, len(director.Objectives))
	for _, objective := range director.Objectives {
		directorParents[objective["title"].(string)] = objective["parentObjectiveId"]
	}
	if director.Characters == nil || director.Instructions == nil || len(director.Objectives) != 5 || directorParents["Reach the lighthouse"] != storm.ID || directorParents["Receive the distress signal"] != crew.ID {
		t.Fatalf("director contract is not array-safe: %+v", director)
	}
}
