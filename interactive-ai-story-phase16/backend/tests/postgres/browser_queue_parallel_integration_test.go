package postgres_test

import (
	"errors"
	"testing"
	"time"

	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/jobqueue"
)

func TestBrowserQueueClaimsDifferentTimelinesWithoutBlockingLocalProvider(t *testing.T) {
	ctx, tx, story, localTimeline := memoryOutboxFixture(t)
	// Shadow only queue tables, so claim cleanup cannot inspect or change real jobs.
	for _, table := range []string{"generation_jobs", "generation_updates"} {
		if _, err := tx.Exec(ctx, "CREATE TEMP TABLE "+table+" (LIKE public."+table+" INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES) ON COMMIT DROP"); err != nil {
			t.Fatal(err)
		}
	}
	newTimeline := func() id.ID {
		tid, _ := id.New()
		if _, err := tx.Exec(ctx, `INSERT INTO timelines(id,story_id,name,head_event_seq) VALUES($1,$2,'browser queue test',2)`, tid, story); err != nil {
			t.Fatal(err)
		}
		return tid
	}
	one, two, three := newTimeline(), newTimeline(), newTimeline()
	now := time.Now().UTC()
	add := func(provider, status string, tid id.ID) id.ID {
		jid, _ := id.New()
		if _, err := tx.Exec(ctx, `INSERT INTO generation_jobs(id,story_id,timeline_id,expected_head_event_seq,config_revision_id,status,provider_kind,provider_name,model_name,action_hash,max_attempts,available_at,lease_expires_at,lease_owner,attempt_count)
 VALUES($1,$2,$3,(SELECT head_event_seq FROM timelines WHERE id=$3),$1,$4,'story_llm',$5,'test',repeat('a',64),1,$6,CASE WHEN $4='running' THEN $7::timestamptz ELSE NULL END,CASE WHEN $4='running' THEN 'local-test' ELSE NULL END,CASE WHEN $4='running' THEN 1 ELSE 0 END)`, jid, story, tid, status, provider, now.Add(-time.Second), now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		return jid
	}
	local := add("openai_compatible", "running", localTimeline)
	first := add("chatgpt_browser", "queued", one)
	second := add("chatgpt_browser", "queued", two)
	add("openai_compatible", "queued", three)
	q := pg.NewJobQueue(tx)
	a, err := q.ClaimNext(ctx, "browser-1", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	b, err := q.ClaimNext(ctx, "browser-2", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if a.Job.ID == b.Job.ID || (a.Job.ID != first && a.Job.ID != second) || (b.Job.ID != first && b.Job.ID != second) {
		t.Fatal("browser claims blocked or duplicated")
	}
	if _, err := tx.Exec(ctx, `UPDATE timelines SET head_event_seq=3 WHERE id=$1`, one); err != nil {
		t.Fatal(err)
	}
	add("chatgpt_browser", "queued", one)
	if _, err := q.ClaimNext(ctx, "browser-3", now, time.Minute); !errors.Is(err, jobqueue.ErrNoJob) {
		t.Fatal("same timeline or busy local GPU claimed", err)
	}
	if err := q.Complete(ctx, a.Job.ID, "browser-1", now); err != nil {
		t.Fatal(err)
	}
	if err := q.Complete(ctx, b.Job.ID, "browser-2", now); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ClaimNext(ctx, "browser-3", now, time.Minute); err != nil {
		t.Fatal("queued timeline did not unlock", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE generation_jobs SET status='completed',lease_owner=NULL,lease_expires_at=NULL WHERE id=$1`, local); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ClaimNext(ctx, "local", now, time.Minute); err != nil {
		t.Fatal("browser job blocked idle local GPU", err)
	}
}
