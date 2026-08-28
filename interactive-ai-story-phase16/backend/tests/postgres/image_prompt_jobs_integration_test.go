package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	pgadapter "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

func TestBeatCommitCreatesRecoverableImagePromptJob(t *testing.T) {
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
	chapterID, _ := id.New()
	sceneID, _ := id.New()
	beatID, _ := id.New()
	eventID, _ := id.New()
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM image_prompt_jobs WHERE story_id=$1`, storyID)
	}()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users(id,username) VALUES($1,$2)`, []any{ownerID, "prompt-job-" + ownerID.String()}},
		{`INSERT INTO stories(id,owner_id,title) VALUES($1,$2,'Prompt job')`, []any{storyID, ownerID}},
		{`INSERT INTO timelines(id,story_id,name,head_event_seq) VALUES($1,$2,'Main',1)`, []any{timelineID, storyID}},
		{`INSERT INTO chapters(id,timeline_id,number,title,status) VALUES($1,$2,1,'Opening','active')`, []any{chapterID, timelineID}},
		{`INSERT INTO scenes(id,chapter_id,number,status) VALUES($1,$2,1,'active')`, []any{sceneID, chapterID}},
		{`INSERT INTO beats(id,scene_id,position,kind,content,status,is_active) VALUES($1,$2,1,'mixed','{"text":"opening"}','committed',true)`, []any{beatID, sceneID}},
		{`INSERT INTO story_events(id,timeline_id,seq,event_type,schema_version,payload) VALUES($1,$2,1,'beat_committed',1,jsonb_build_object('beatId',$3::text,'sceneId',$4::text,'position',1,'text','opening'))`, []any{eventID, timelineID, beatID, sceneID}},
	}
	for _, statement := range statements {
		if _, err = pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	// Keep the global queue deterministic without touching jobs belonging to
	// real stories or other integration cases.
	if _, err = pool.Exec(ctx, `UPDATE image_prompt_jobs SET created_at='2000-01-01 UTC',available_at='2000-01-01 UTC' WHERE source_beat_id=$1`, beatID); err != nil {
		t.Fatal(err)
	}

	repo := pgadapter.NewImageGenerationRepository(pool)
	now := time.Now().UTC()
	job, err := repo.ClaimPromptJob(ctx, "prompt-worker-test", ownerID, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if job.StoryID != storyID || job.TimelineID != timelineID || job.SceneID != sceneID || job.SourceBeatID != beatID || !job.ForceIllustration {
		t.Fatalf("unexpected prompt job: %+v", job)
	}
	if err = repo.FailPromptJob(ctx, job.ID, "prompt-worker-test", now, 0, "retry me"); err != nil {
		t.Fatal(err)
	}
	job, err = repo.ClaimPromptJob(ctx, "prompt-worker-test", ownerID, now.Add(time.Millisecond), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if job.Attempts != 2 {
		t.Fatalf("prompt job attempts=%d want=2", job.Attempts)
	}
	if err = repo.CompletePromptJob(ctx, job.ID, "prompt-worker-test", now.Add(2*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM image_prompt_jobs WHERE id=$1`, job.ID).Scan(&status); err != nil || status != "completed" {
		t.Fatalf("prompt job status=%q err=%v", status, err)
	}
}
