package postgres_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestSetupGenerationPopulatesActionHash(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := bootstrap.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = bootstrap.EnsureAIConfig(ctx, pool, ai.ProviderIdentity{Kind: ai.KindStoryLLM, Provider: "openai_compatible", Model: "integration-test", Profile: "test"}); err != nil {
		t.Fatal(err)
	}
	if err = bootstrap.EnsurePromptSet(ctx, pool); err != nil {
		t.Fatal(err)
	}
	owner, _ := id.New()
	sid, _ := id.New()
	gid, _ := id.New()
	if _, err = pool.Exec(ctx, `INSERT INTO users(id,username) VALUES($1,$2)`, owner, "setup-hash-test-"+owner.String()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		for _, q := range []struct {
			sql   string
			value id.ID
		}{
			{`DELETE FROM generation_jobs WHERE id=$1`, gid},
			{`DELETE FROM stories WHERE id=$1`, sid},
			{`DELETE FROM users WHERE id=$1`, owner},
		} {
			if _, e := pool.Exec(cleanup, q.sql, q.value); e != nil {
				t.Error(e)
			}
		}
	}()
	repo := pg.NewSetupRepository(pool)
	s, err := story.New(story.ID(sid), story.UserID(owner), "Setup hash regression", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.CreateStory(ctx, s, nil); err != nil {
		t.Fatal(err)
	}
	if err = repo.CreateSetupGeneration(ctx, event.GenerationID(gid), story.ID(sid), "test", "test", "test"); err != nil {
		t.Fatal(err)
	}
	var hash, status string
	if err = pool.QueryRow(ctx, `SELECT action_hash,status FROM generation_jobs WHERE id=$1`, gid).Scan(&hash, &status); err != nil {
		t.Fatal(err)
	}
	if hash != fmt.Sprintf("%x", sha256.Sum256(nil)) || status != "running" {
		t.Fatalf("hash=%q status=%q", hash, status)
	}
	if err = repo.FinishSetupGeneration(ctx, event.GenerationID(gid), "completed", nil); err != nil {
		t.Fatal(err)
	}
}
