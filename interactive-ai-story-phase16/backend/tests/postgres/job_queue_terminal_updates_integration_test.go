package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

type terminalUpdate struct {
	count int
	phase string
	err   string
	seq   int64
}

func lastJobUpdate(t *testing.T, ctx context.Context, tx pgx.Tx, job id.ID) terminalUpdate {
	t.Helper()
	var u terminalUpdate
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM generation_updates WHERE generation_job_id=$1`, job).Scan(&u.count); err != nil {
		t.Fatal(err)
	}
	if u.count == 0 {
		return u
	}
	if err := tx.QueryRow(ctx, `SELECT phase,error_text,sequence FROM generation_updates WHERE generation_job_id=$1 ORDER BY sequence DESC LIMIT 1`, job).Scan(&u.phase, &u.err, &u.seq); err != nil {
		t.Fatal(err)
	}
	return u
}

// C2: a job that ends failed/cancelled in the queue must leave a terminal
// update in generation_updates so durable SSE replay always finishes.
func TestJobQueueWritesTerminalUpdateOnFinalFailure(t *testing.T) {
	ctx, tx, story, tid := memoryOutboxFixture(t)
	var config id.ID
	if err := tx.QueryRow(ctx, `INSERT INTO ai_config_revisions(story_llm_provider,story_llm_model,story_llm_profile,embedding_provider,embedding_model,image_provider,image_model,settings)
 VALUES('openai_compatible','test','test','fake','384','manual','manual-v1','{}') RETURNING id`).Scan(&config); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	newJob := func(head, attempts int, leaseExpires time.Time, lastPhase string) id.ID {
		t.Helper()
		job, _ := id.New()
		if _, err := tx.Exec(ctx, `INSERT INTO generation_jobs(id,story_id,timeline_id,expected_head_event_seq,config_revision_id,status,provider_kind,provider_name,model_name,action_hash,attempt_count,max_attempts,lease_owner,lease_expires_at,heartbeat_at,started_at)
 VALUES($1,$2,$3,$4,$5,'running','story_llm','fake','test',repeat('0',64),$6,3,'w',$7,$8,$8)`, job, story, tid, head, config, attempts, leaseExpires, now.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
		if lastPhase != "" {
			if _, err := tx.Exec(ctx, `INSERT INTO generation_updates(generation_job_id,sequence,phase) VALUES($1,1,$2)`, job, lastPhase); err != nil {
				t.Fatal(err)
			}
		}
		return job
	}
	queue := pg.NewJobQueue(tx)

	// FailOrRetry on the last attempt appends a terminal failed update.
	exhausted := newJob(1001, 3, now.Add(time.Minute), "writing")
	if err := queue.FailOrRetry(ctx, exhausted, "w", now, time.Second, "provider down"); err != nil {
		t.Fatal(err)
	}
	if u := lastJobUpdate(t, ctx, tx, exhausted); u.count != 2 || u.phase != "failed" || u.err != "provider down" || u.seq != 2 {
		t.Fatalf("exhausted job last update = %+v, want failed/provider down at seq 2", u)
	}

	// No duplicate when the pipeline already persisted the terminal update.
	alreadyFailed := newJob(1002, 3, now.Add(time.Minute), "failed")
	if err := queue.FailOrRetry(ctx, alreadyFailed, "w", now, time.Second, "provider down"); err != nil {
		t.Fatal(err)
	}
	if u := lastJobUpdate(t, ctx, tx, alreadyFailed); u.count != 1 {
		t.Fatalf("terminal update duplicated: %+v", u)
	}

	// A retryable failure stays queued and gets no terminal update.
	retryable := newJob(1003, 1, now.Add(time.Minute), "retrying")
	if err := queue.FailOrRetry(ctx, retryable, "w", now, time.Second, "provider down"); err != nil {
		t.Fatal(err)
	}
	if u := lastJobUpdate(t, ctx, tx, retryable); u.count != 1 || u.phase != "retrying" {
		t.Fatalf("retryable job got terminal update: %+v", u)
	}

	// Lease recovery: an exhausted expired job gets 'lease expired'; a
	// requeued one does not.
	expiredExhausted := newJob(1004, 3, now.Add(-time.Second), "")
	expiredRetryable := newJob(1005, 1, now.Add(-time.Second), "")
	recovered, err := queue.RequeueExpired(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if recovered < 2 {
		t.Fatalf("recovered = %d, want >= 2", recovered)
	}
	if u := lastJobUpdate(t, ctx, tx, expiredExhausted); u.count != 1 || u.phase != "failed" || u.err != "lease expired" || u.seq != 1 {
		t.Fatalf("expired exhausted job last update = %+v, want failed/lease expired", u)
	}
	if u := lastJobUpdate(t, ctx, tx, expiredRetryable); u.count != 0 {
		t.Fatalf("requeued job got terminal update: %+v", u)
	}

	// User cancellation finishes through Complete rather than FailOrRetry.
	// A prior retrying frame must not be left as the final durable update.
	cancelled := newJob(1006, 1, now.Add(time.Minute), "retrying")
	if err := queue.RequestCancel(ctx, cancelled, now); err != nil {
		t.Fatal(err)
	}
	if err := queue.Complete(ctx, cancelled, "w", now); err != nil {
		t.Fatal(err)
	}
	if u := lastJobUpdate(t, ctx, tx, cancelled); u.count != 2 || u.phase != "failed" || u.err != "generation cancelled" {
		t.Fatalf("cancelled job lacks a durable terminal update: %+v", u)
	}
}
