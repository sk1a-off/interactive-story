package postgres_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/memoryindexrepo"
)

func memoryOutboxFixture(t *testing.T) (context.Context, pgx.Tx, id.ID, id.ID) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	pool, err := bootstrap.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	owner, _ := id.New()
	story, _ := id.New()
	timeline, _ := id.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users(id,username) VALUES($1,$2)`, []any{owner, "memory-outbox-" + owner.String()}},
		{`INSERT INTO stories(id,owner_id,title) VALUES($1,$2,'memory outbox fixture')`, []any{story, owner}},
		{`INSERT INTO timelines(id,story_id,name,head_event_seq) VALUES($1,$2,'root',2)`, []any{timeline, story}},
	} {
		if _, err = tx.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	return ctx, tx, story, timeline
}

func TestMemoryOutboxIsAtomicScopedAndMonotonic(t *testing.T) {
	ctx, tx, story, timeline := memoryOutboxFixture(t)
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	eventID, _ := id.New()
	insertEvent := `INSERT INTO story_events(id,timeline_id,seq,event_type,payload) VALUES($1,$2,$3,'player_action_attempted','{"text":"fixture"}')`
	if _, err = savepoint.Exec(ctx, insertEvent, eventID, timeline, 1); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = savepoint.QueryRow(ctx, `SELECT count(*) FROM memory_index_jobs WHERE story_id=$1`, story).Scan(&count); err != nil || count != 1 {
		t.Fatalf("atomic job count=%d error=%v", count, err)
	}
	if err = savepoint.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM memory_index_jobs WHERE story_id=$1`, story).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled-back job remains count=%d error=%v", count, err)
	}
	if _, err = tx.Exec(ctx, insertEvent, eventID, timeline, 2); err != nil {
		t.Fatal(err)
	}
	snapshotID, _ := id.New()
	if _, err = tx.Exec(ctx, `INSERT INTO snapshots(id,timeline_id,event_seq,state,state_hash) VALUES($1,$2,0,'{}',repeat('0',64))`, snapshotID, timeline); err != nil {
		t.Fatal(err)
	}
	var cursor int64
	if err = tx.QueryRow(ctx, `SELECT observed_event_seq FROM memory_index_state WHERE timeline_id=$1`, timeline).Scan(&cursor); err != nil || cursor != 2 {
		t.Fatalf("snapshot regressed dirty cursor=%d error=%v", cursor, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO memory_index_jobs(story_id,timeline_id,source_kind,source_event_id,source_event_seq) VALUES($1,$2,'event',$3,2) ON CONFLICT(source_event_id) DO NOTHING`, story, timeline, eventID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM memory_index_jobs WHERE story_id=$1`, story).Scan(&count); err != nil || count != 2 {
		t.Fatalf("duplicate source job count=%d error=%v", count, err)
	}
	savepoint, err = tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = savepoint.Exec(ctx, `UPDATE memory_index_jobs SET source_event_seq=999 WHERE source_event_id=$1`, eventID)
	if err == nil || !strings.Contains(err.Error(), "source is immutable") {
		t.Fatalf("source mutation accepted: %v", err)
	}
	if err = savepoint.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryIndexQueueRecoveryFencingOrderingAndExhaustion(t *testing.T) {
	ctx, tx, story, timeline := memoryOutboxFixture(t)
	for seq := 1; seq <= 2; seq++ {
		if _, err := tx.Exec(ctx, `INSERT INTO story_events(timeline_id,seq,event_type,payload) VALUES($1,$2,'player_action_attempted','{}')`, timeline, seq); err != nil {
			t.Fatal(err)
		}
	}
	queue := pg.NewMemoryIndexQueue(tx)
	queue.StoryID = story
	now := time.Now().UTC().Add(time.Minute)
	first, err := queue.Claim(ctx, "worker", now, time.Second)
	if err != nil || first.SourceEventSeq != 1 || first.Attempts != 1 {
		t.Fatalf("first=%+v error=%v", first, err)
	}
	if _, err = queue.Claim(ctx, "other", now, time.Second); !errors.Is(err, repo.ErrNoJob) {
		t.Fatalf("same timeline indexed out of order: %v", err)
	}
	late, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = late.Exec(ctx, `INSERT INTO snapshots(timeline_id,event_seq,state,state_hash) VALUES($1,0,'{}',repeat('0',64))`, timeline); err != nil {
		t.Fatal(err)
	}
	lateQueue := pg.NewMemoryIndexQueue(late)
	lateQueue.StoryID = story
	if _, err = lateQueue.Claim(ctx, "other", now, time.Second); !errors.Is(err, repo.ErrNoJob) {
		t.Fatalf("late old snapshot ran concurrently with newer event: %v", err)
	}
	if err = late.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	wrong := first
	wrong.LeaseOwner = "other"
	if err = queue.Complete(ctx, wrong, now); !errors.Is(err, repo.ErrLeaseLost) {
		t.Fatalf("foreign worker completed job: %v", err)
	}
	second, err := queue.Claim(ctx, "worker", now.Add(2*time.Second), time.Minute)
	if err != nil || second.ID != first.ID || second.LeaseToken == first.LeaseToken || second.Attempts != 2 {
		t.Fatalf("recovery=%+v error=%v", second, err)
	}
	if err = queue.Complete(ctx, first, now.Add(2*time.Second)); !errors.Is(err, repo.ErrLeaseLost) {
		t.Fatalf("old token completed reclaimed job: %v", err)
	}
	if err = queue.Heartbeat(ctx, first, now.Add(2*time.Second), time.Minute); !errors.Is(err, repo.ErrLeaseLost) {
		t.Fatalf("old token renewed reclaimed job: %v", err)
	}
	if err = queue.Heartbeat(ctx, second, now.Add(2*time.Second), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err = queue.Retry(ctx, second, now.Add(2*time.Second), 10*time.Second, "temporary embedding failure"); err != nil {
		t.Fatal(err)
	}
	if _, err = queue.Claim(ctx, "worker", now.Add(3*time.Second), time.Minute); !errors.Is(err, repo.ErrNoJob) {
		t.Fatalf("backoff or timeline order ignored: %v", err)
	}
	third, err := queue.Claim(ctx, "worker", now.Add(13*time.Second), time.Minute)
	if err != nil || third.ID != first.ID || third.Attempts != 3 {
		t.Fatalf("retry=%+v error=%v", third, err)
	}
	if err = queue.Complete(ctx, third, now.Add(13*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE memory_index_jobs SET max_attempts=1 WHERE timeline_id=$1 AND source_event_seq=2`, timeline); err != nil {
		t.Fatal(err)
	}
	last, err := queue.Claim(ctx, "worker", now.Add(13*time.Second), time.Second)
	if err != nil || last.SourceEventSeq != 2 {
		t.Fatalf("next=%+v error=%v", last, err)
	}
	if _, err = queue.Claim(ctx, "worker", now.Add(15*time.Second), time.Minute); !errors.Is(err, repo.ErrNoJob) {
		t.Fatalf("exhausted job reclaimed: %v", err)
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM memory_index_jobs WHERE id=$1`, last.ID).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("no-job recovery rolled back: status=%s error=%v", status, err)
	}
	if err = queue.Complete(ctx, last, now.Add(15*time.Second)); !errors.Is(err, repo.ErrLeaseLost) {
		t.Fatalf("expired worker completed terminal job: %v", err)
	}
}
