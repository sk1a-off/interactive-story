package postgres_test

import (
	"errors"
	"testing"
	"time"

	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/memoryindexrepo"
)

// A job that exhausted its attempts used to stay failed forever and, because
// claims are ordered per timeline, blocked every later job of that timeline.
// It must now be recovered after a bounded, escalating cooldown without
// skipping any source event, and give up explicitly after the recovery cap.
func TestMemoryIndexFailedJobsRecoverInOrderWithBoundedRetries(t *testing.T) {
	ctx, tx, story, timeline := memoryOutboxFixture(t)
	insertEvent := `INSERT INTO story_events(id,timeline_id,seq,event_type,payload) VALUES($1,$2,$3,'player_action_attempted','{"text":"fixture"}')`
	firstEvent, _ := id.New()
	secondEvent, _ := id.New()
	if _, err := tx.Exec(ctx, insertEvent, firstEvent, timeline, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, insertEvent, secondEvent, timeline, 4); err != nil {
		t.Fatal(err)
	}
	// Legacy-style jobs without an index profile (index_revision NULL).
	if _, err := tx.Exec(ctx, `UPDATE memory_index_jobs SET max_attempts=1 WHERE timeline_id=$1`, timeline); err != nil {
		t.Fatal(err)
	}
	queue := pg.NewMemoryIndexQueue(tx)
	queue.StoryID = story // the shared test database holds committed rows from other tests
	now := time.Now().UTC().Truncate(time.Microsecond)

	first, err := queue.Claim(ctx, "worker", now, time.Minute)
	if err != nil || first.SourceEventSeq != 3 {
		t.Fatalf("first claim=%+v error=%v", first, err)
	}
	if err := queue.Retry(ctx, first, now, time.Second, "embedding provider down"); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM memory_index_jobs WHERE id=$1`, first.ID).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("exhausted job status=%s error=%v", status, err)
	}
	// The later job is blocked behind the failed one (ordering guarantee).
	if _, err := queue.Claim(ctx, "worker", now.Add(time.Second), time.Minute); !errors.Is(err, repo.ErrNoJob) {
		t.Fatalf("later job claimed ahead of a failed predecessor: %v", err)
	}

	// Before the cooldown nothing is recovered; after it the same job returns.
	if recovered, err := queue.RecoverFailed(ctx, now.Add(30*time.Second), pg.MaxMemoryIndexRecoveries); err != nil || recovered != 0 {
		t.Fatalf("recovered before cooldown: n=%d err=%v", recovered, err)
	}
	if recovered, err := queue.RecoverFailed(ctx, now.Add(61*time.Second), pg.MaxMemoryIndexRecoveries); err != nil || recovered != 1 {
		t.Fatalf("recovery after cooldown: n=%d err=%v", recovered, err)
	}
	again, err := queue.Claim(ctx, "worker", now.Add(62*time.Second), time.Minute)
	if err != nil || again.ID != first.ID || again.Attempts != 1 {
		t.Fatalf("recovered job not reclaimed first: %+v error=%v", again, err)
	}
	var recoveries int
	var lastError string
	if err := tx.QueryRow(ctx, `SELECT recoveries,last_error FROM memory_index_jobs WHERE id=$1`, first.ID).Scan(&recoveries, &lastError); err != nil || recoveries != 1 || lastError == "" {
		t.Fatalf("recovery bookkeeping: recoveries=%d last_error=%q err=%v", recoveries, lastError, err)
	}
	if err := queue.Complete(ctx, again, now.Add(63*time.Second)); err != nil {
		t.Fatal(err)
	}
	// Once the predecessor completes, the blocked job flows again in order.
	second, err := queue.Claim(ctx, "worker", now.Add(64*time.Second), time.Minute)
	if err != nil || second.SourceEventSeq != 4 {
		t.Fatalf("second claim=%+v error=%v", second, err)
	}

	// Cooldown escalates with the recovery count and stops at the cap.
	if err := queue.Retry(ctx, second, now.Add(64*time.Second), time.Second, "still down"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE memory_index_jobs SET recoveries=$2 WHERE id=$1`, second.ID, pg.MaxMemoryIndexRecoveries-1); err != nil {
		t.Fatal(err)
	}
	// recoveries=7 → cooldown min(2^7 min, 1h) = 1h.
	if recovered, err := queue.RecoverFailed(ctx, now.Add(64*time.Second+59*time.Minute), pg.MaxMemoryIndexRecoveries); err != nil || recovered != 0 {
		t.Fatalf("escalated cooldown ignored: n=%d err=%v", recovered, err)
	}
	if recovered, err := queue.RecoverFailed(ctx, now.Add(64*time.Second+61*time.Minute), pg.MaxMemoryIndexRecoveries); err != nil || recovered != 1 {
		t.Fatalf("final recovery: n=%d err=%v", recovered, err)
	}
	last, err := queue.Claim(ctx, "worker", now.Add(64*time.Second+62*time.Minute), time.Minute)
	if err != nil || last.ID != second.ID {
		t.Fatalf("final attempt claim=%+v error=%v", last, err)
	}
	if err := queue.Retry(ctx, last, now.Add(64*time.Second+62*time.Minute), time.Second, "permanently broken"); err != nil {
		t.Fatal(err)
	}
	if recovered, err := queue.RecoverFailed(ctx, now.Add(48*time.Hour), pg.MaxMemoryIndexRecoveries); err != nil || recovered != 0 {
		t.Fatalf("exhausted job must not be recovered: n=%d err=%v", recovered, err)
	}
	if err := tx.QueryRow(ctx, `SELECT status,recoveries FROM memory_index_jobs WHERE id=$1`, second.ID).Scan(&status, &recoveries); err != nil || status != "failed" || recoveries != pg.MaxMemoryIndexRecoveries {
		t.Fatalf("exhausted job state=%s recoveries=%d err=%v", status, recoveries, err)
	}
}
