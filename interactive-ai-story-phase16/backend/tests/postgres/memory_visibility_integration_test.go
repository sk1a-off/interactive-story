package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/memory"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

func TestPersistedMemoryLineageExcludesParentFutureAndSiblings(t *testing.T) {
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
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	newID := func() id.ID {
		t.Helper()
		value, err := id.New()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	owner, story, root, child, nested, sibling := newID(), newID(), newID(), newID(), newID(), newID()
	exec(`INSERT INTO users(id,username) VALUES($1,$2)`, owner, "memory-lineage-"+owner.String())
	exec(`INSERT INTO stories(id,owner_id,title) VALUES($1,$2,'memory lineage fixture')`, story, owner)
	exec(`INSERT INTO timelines(id,story_id,name,head_event_seq) VALUES($1,$2,'root',200)`, root, story)
	fork := func(parent, child id.ID, seq, head int64) {
		t.Helper()
		snap, save := newID(), newID()
		// These SQL fixtures exercise lineage constraints only, not snapshot replay.
		exec(`INSERT INTO snapshots(id,timeline_id,event_seq,state,state_hash) VALUES($1,$2,$3,'{}',repeat('0',64))`, snap, parent, seq)
		exec(`INSERT INTO save_points(id,story_id,timeline_id,snapshot_id,event_seq,name) VALUES($1,$2,$3,$4,$5,'fixture')`, save, story, parent, snap, seq)
		exec(`INSERT INTO timelines(id,story_id,parent_timeline_id,forked_from_save_id,name,head_event_seq) VALUES($1,$2,$3,$4,'child',$5)`, child, story, parent, save, seq)
		exec(`UPDATE timelines SET head_event_seq=$2 WHERE id=$1`, child, head)
	}
	fork(root, child, 100, 160)
	fork(child, nested, 130, 150)
	fork(root, sibling, 60, 60)
	resolver := pg.NewMemoryVisibilityResolver(tx)
	plan, err := resolver.Resolve(ctx, story, timeline.ID(nested), 140)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.AllowsHistorical(story, timeline.ID(root), 100) || plan.AllowsHistorical(story, timeline.ID(root), 101) ||
		!plan.AllowsHistorical(story, timeline.ID(child), 130) || plan.AllowsHistorical(story, timeline.ID(child), 131) ||
		plan.AllowsHistorical(story, timeline.ID(sibling), 60) {
		t.Fatalf("unexpected inherited history: %+v", plan.Segments())
	}
	restored, err := resolver.Resolve(ctx, story, timeline.ID(sibling), 60)
	if err != nil || restored.AllowsHistorical(story, timeline.ID(root), 61) {
		t.Fatalf("restore future leak: %v", err)
	}
	for _, request := range []struct {
		story    id.ID
		timeline timeline.ID
		at       int64
	}{
		{newID(), timeline.ID(nested), 140}, {story, timeline.ID(newID()), 1},
		{story, timeline.ID(nested), 129}, {story, timeline.ID(nested), 151},
	} {
		if _, err := resolver.Resolve(ctx, request.story, request.timeline, request.at); !errors.Is(err, domain.ErrInvalidVisibility) {
			t.Fatalf("invalid scope was not rejected: %+v error=%v", request, err)
		}
	}
}
