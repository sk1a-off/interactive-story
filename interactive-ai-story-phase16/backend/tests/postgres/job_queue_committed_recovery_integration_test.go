package postgres_test

import (
	"errors"
	"testing"
	"time"

	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/jobqueue"
)

// A turn whose events reached Canon must finish as completed even when the
// worker died (or Complete failed) before finalizing the job. Previously lease
// recovery requeued it and the stale-head rule then reported it as failed.
func TestJobQueueFinalizesCommittedTurnsInsteadOfFailingThem(t *testing.T) {
	ctx, tx, story, tid := memoryOutboxFixture(t)
	var config id.ID
	if err := tx.QueryRow(ctx, `INSERT INTO ai_config_revisions(story_llm_provider,story_llm_model,story_llm_profile,embedding_provider,embedding_model,image_provider,image_model,settings)
 VALUES('openai_compatible','test','test','fake','384','manual','manual-v1','{}') RETURNING id`).Scan(&config); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	newJob := func(status string, head int, leaseExpires *time.Time, lastPhase string) id.ID {
		t.Helper()
		job, _ := id.New()
		if _, err := tx.Exec(ctx, `INSERT INTO generation_jobs(id,story_id,timeline_id,expected_head_event_seq,config_revision_id,status,provider_kind,provider_name,model_name,action_hash,attempt_count,max_attempts,lease_owner,lease_expires_at,heartbeat_at,started_at)
 VALUES($1,$2,$3,$4,$5,$6,'story_llm','fake','test',repeat('0',64),1,3,CASE WHEN $7::timestamptz IS NULL THEN NULL ELSE 'w' END,$7,$8,$8)`, job, story, tid, head, config, status, leaseExpires, now.Add(-time.Minute)); err != nil {
			t.Fatal(err)
		}
		if lastPhase != "" {
			if _, err := tx.Exec(ctx, `INSERT INTO generation_updates(generation_job_id,sequence,phase) VALUES($1,1,$2)`, job, lastPhase); err != nil {
				t.Fatal(err)
			}
		}
		return job
	}
	commitEvents := func(job id.ID, seq int) {
		t.Helper()
		eventID, _ := id.New()
		if _, err := tx.Exec(ctx, `INSERT INTO story_events(id,timeline_id,seq,event_type,schema_version,payload,generation_id) VALUES($1,$2,$3,'player_action_attempted',1,'{}',$4)`, eventID, tid, seq, job); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE timelines SET head_event_seq=$2 WHERE id=$1`, tid, seq); err != nil {
			t.Fatal(err)
		}
	}
	queue := pg.NewJobQueue(tx)

	// Worker died after the Canon commit: running job, expired lease, events present.
	expired := now.Add(-time.Second)
	crashedAfterCommit := newJob("running", 2, &expired, "committing")
	commitEvents(crashedAfterCommit, 3)
	if _, err := queue.RequeueExpired(ctx, now); err != nil {
		t.Fatal(err)
	}
	var status string
	var lastError *string
	if err := tx.QueryRow(ctx, `SELECT status,last_error FROM generation_jobs WHERE id=$1`, crashedAfterCommit).Scan(&status, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || lastError != nil {
		t.Fatalf("committed turn after lease loss: status=%s last_error=%v, want completed", status, lastError)
	}
	if u := lastJobUpdate(t, ctx, tx, crashedAfterCommit); u.count != 2 || u.phase != "completed" {
		t.Fatalf("committed turn lacks a durable completed update: %+v", u)
	}

	// Same job already requeued by older code: it is now stale against the
	// moved head but its events exist, so the claim pass completes it.
	requeuedAfterCommit := newJob("queued", 3, nil, "finalizing")
	commitEvents(requeuedAfterCommit, 4)
	// A genuinely stale queued action without events must still be cancelled.
	staleWithoutEvents := newJob("queued", 1, nil, "")
	if _, err := queue.ClaimNext(ctx, "w", now, time.Minute); err != nil && !errors.Is(err, jobqueue.ErrNoJob) {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT status FROM generation_jobs WHERE id=$1`, requeuedAfterCommit).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "completed" {
		t.Fatalf("requeued committed turn status=%s, want completed", status)
	}
	if u := lastJobUpdate(t, ctx, tx, requeuedAfterCommit); u.count != 2 || u.phase != "completed" {
		t.Fatalf("requeued committed turn last update = %+v, want completed", u)
	}
	var errorCode string
	if err := tx.QueryRow(ctx, `SELECT status,COALESCE(error_code,'') FROM generation_jobs WHERE id=$1`, staleWithoutEvents).Scan(&status, &errorCode); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || errorCode != "stale_head" {
		t.Fatalf("stale action without events: status=%s code=%s, want cancelled/stale_head", status, errorCode)
	}

	// Pipeline already published its own terminal update: no duplicate frame.
	alreadyTerminal := newJob("running", 4, &expired, "completed")
	commitEvents(alreadyTerminal, 5)
	if _, err := queue.RequeueExpired(ctx, now); err != nil {
		t.Fatal(err)
	}
	if u := lastJobUpdate(t, ctx, tx, alreadyTerminal); u.count != 1 || u.phase != "completed" {
		t.Fatalf("terminal update duplicated: %+v", u)
	}
}
