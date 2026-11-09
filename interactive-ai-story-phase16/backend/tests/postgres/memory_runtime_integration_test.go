package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/application/memoryindex"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
	indexrepo "github.com/local/interactive-ai-story/backend/internal/ports/memoryindexrepo"
)

func TestMemoryRuntimeBuildsIndependentProfilesAndPublishesOnlyWhenComplete(t *testing.T) {
	ctx, tx, story, tid := memoryOutboxFixture(t)
	var owner, config id.ID
	if err := tx.QueryRow(ctx, `SELECT owner_id FROM stories WHERE id=$1`, story).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO ai_config_revisions(story_llm_provider,story_llm_model,story_llm_profile,embedding_provider,embedding_model,image_provider,image_model,settings)
 VALUES('openai_compatible','test','test','fake','deterministic-embedding-v1','manual','manual-v1','{}') RETURNING id`).Scan(&config); err != nil {
		t.Fatal(err)
	}
	factID, _ := id.New()
	payload, _ := json.Marshal(map[string]any{"commandType": "upsert_fact", "after": map[string]any{"ID": factID, "SubjectType": "world", "Predicate": "door", "Object": "locked"}})
	if _, err := tx.Exec(ctx, `INSERT INTO story_events(timeline_id,seq,event_type,payload) VALUES($1,1,'director_exact_edit_applied',$2),($1,2,'player_action_attempted','{}')`, tid, payload); err != nil {
		t.Fatal(err)
	}
	state := pg.NewMemoryRuntimeRepository(tx)
	state.OwnerID = owner
	e := fakeai.NewEmbeddingProvider(384)
	p1, err := state.Ensure(ctx, config, "signature", e.ID.Provider, e.ID.Model, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.Serving(ctx, "signature"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("incomplete profile served")
	}
	run := func(revision int64, n int) {
		t.Helper()
		q := pg.NewMemoryIndexQueue(tx)
		q.IndexRevision = revision
		w := memoryindex.Worker{Queue: q, Store: pg.NewMemoryIndexStore(tx), Embeddings: e, Profile: indexrepo.Profile{Provider: e.ID.Provider, Model: e.ID.Model, Revision: revision}, Owner: "runtime-test", Now: func() time.Time { return time.Now().Add(time.Minute) }}
		for range n {
			if err := w.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	run(p1.Revision, 1)
	if err = state.Finalize(ctx, p1.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = state.Serving(ctx, "signature"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("partial profile published")
	}
	run(p1.Revision, 1)
	if err = state.Finalize(ctx, p1.Revision); err != nil {
		t.Fatal(err)
	}
	if p, err := state.Serving(ctx, "signature"); err != nil || p.Revision != p1.Revision {
		t.Fatalf("profile not published: %+v %v", p, err)
	}
	if e.Calls != 1 {
		t.Fatal("profile did not embed its source document")
	}
	p2, err := state.Ensure(ctx, config, "signature", e.ID.Provider, e.ID.Model, true)
	if err != nil || p2.Revision == p1.Revision {
		t.Fatalf("new revision: %+v %v", p2, err)
	}
	duplicate, err := state.Ensure(ctx, config, "signature", e.ID.Provider, e.ID.Model, true)
	if err != nil || duplicate.Revision != p2.Revision {
		t.Fatal("concurrent rebuild not idempotent")
	}
	if p, err := state.Serving(ctx, "signature"); err != nil || p.Revision != p1.Revision {
		t.Fatal("rebuild removed working profile")
	}
	if _, err = tx.Exec(ctx, `UPDATE timelines SET head_event_seq=3 WHERE id=$1`, tid); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO story_events(timeline_id,seq,event_type,payload) VALUES($1,3,'player_action_attempted','{}')`, tid); err != nil {
		t.Fatal(err)
	}
	var copies int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM memory_index_jobs WHERE timeline_id=$1 AND source_event_seq=3`, tid).Scan(&copies); err != nil || copies != 3 {
		t.Fatalf("catalog/serving/building fanout=%d error=%v", copies, err)
	}
	vector := make([]float32, 384)
	vector[0] = 1
	query := memory.Query{StoryID: story, TimelineID: timeline.ID(tid), OwnerType: memory.OwnerTimeline, OwnerID: tid, AtEventSeq: 3, Vector: vector, Text: "door", Limit: 8, Profile: memory.EmbeddingProfile{Provider: e.ID.Provider, Model: e.ID.Model, Revision: p1.Revision}}
	if found, err := pg.NewHybridMemoryRetriever(tx).Search(ctx, query); err != nil || len(found) != 0 {
		t.Fatal("stale serving state exposed while reindexing")
	}
	run(p2.Revision, 3)
	if err = state.Finalize(ctx, p2.Revision); err != nil {
		t.Fatal(err)
	}
	if p, err := state.Serving(ctx, "signature"); err != nil || p.Revision != p2.Revision {
		t.Fatal("atomic switch failed")
	}
	query.Profile.Revision = p2.Revision
	if found, err := pg.NewHybridMemoryRetriever(tx).Search(ctx, query); err != nil || len(found) != 1 || found[0].Kind != "fact" {
		t.Fatalf("new profile retrieval failed: %+v %v", found, err)
	}
	// Retiring an index must not break acknowledgement replay for a job
	// already atomically published under that exact identity.
	replay := indexrepo.Job{StoryID: story, TimelineID: timeline.ID(tid), SourceKind: "event", IndexRevision: p1.Revision}
	if err = tx.QueryRow(ctx, `SELECT id,source_event_id,source_event_seq FROM memory_index_jobs WHERE index_revision=$1 ORDER BY source_event_seq LIMIT 1`, p1.Revision).Scan(&replay.ID, &replay.SourceID, &replay.SourceEventSeq); err != nil {
		t.Fatal(err)
	}
	if err = pg.NewMemoryIndexStore(tx).Publish(ctx, replay, indexrepo.Profile{Provider: e.ID.Provider, Model: e.ID.Model, Revision: p1.Revision}, nil, nil, time.Now()); err != nil {
		t.Fatalf("retired receipt replay failed: %v", err)
	}
	old := pg.NewMemoryIndexQueue(tx)
	old.IndexRevision = p1.Revision
	if _, err = old.Claim(ctx, "stale", time.Now().Add(time.Minute), time.Minute); !errors.Is(err, indexrepo.ErrNoJob) {
		t.Fatal("retired profile claimed work")
	}
	p3, err := state.Ensure(ctx, config, "signature", e.ID.Provider, e.ID.Model, true)
	if err != nil {
		t.Fatal(err)
	}
	q3 := pg.NewMemoryIndexQueue(tx)
	q3.IndexRevision = p3.Revision
	job, err := q3.Claim(ctx, "test", time.Now().Add(time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	wrong := indexrepo.Profile{Provider: "other", Model: e.ID.Model, Revision: p3.Revision}
	if err = pg.NewMemoryIndexStore(tx).Publish(ctx, job, wrong, nil, nil, time.Now().Add(time.Minute)); !errors.Is(err, indexrepo.ErrLeaseLost) {
		t.Fatal("wrong model published job")
	}
	if _, err = tx.Exec(ctx, `UPDATE memory_index_jobs SET status='failed',lease_owner=NULL,lease_token=NULL,lease_until=NULL WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	if err = state.Finalize(ctx, p3.Revision); err != nil {
		t.Fatal(err)
	}
	status, err := state.Status(ctx, "signature")
	if err != nil || status.State != "failed" || status.ServingRevision != p2.Revision {
		t.Fatalf("failed rebuild damaged serving: %+v %v", status, err)
	}
	foreign := pg.NewMemoryRuntimeRepository(tx)
	foreign.OwnerID, _ = id.New()
	if p, err := foreign.Serving(ctx, "signature"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("foreign profile exposed: %+v %v", p, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO users(id,username) VALUES($1,$2)`, foreign.OwnerID, "memory-foreign-"+foreign.OwnerID.String()); err != nil {
		t.Fatal(err)
	}
	foreignStory, _ := id.New()
	foreignTimeline, _ := id.New()
	foreignEvent, _ := id.New()
	if _, err = tx.Exec(ctx, `INSERT INTO stories(id,owner_id,title) VALUES($1,$2,'foreign')`, foreignStory, foreign.OwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO timelines(id,story_id,name,head_event_seq) VALUES($1,$2,'foreign',1)`, foreignTimeline, foreignStory); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO story_events(id,timeline_id,seq,event_type,payload) VALUES($1,$2,1,'player_action_attempted','{}')`, foreignEvent, foreignTimeline); err != nil {
		t.Fatal(err)
	}
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = savepoint.Exec(ctx, `INSERT INTO memory_index_jobs(story_id,timeline_id,source_kind,source_event_id,source_event_seq,index_revision) VALUES($1,$2,'event',$3,1,$4)`, foreignStory, foreignTimeline, foreignEvent, p2.Revision)
	if err == nil {
		t.Fatal("foreign owner source assigned to profile")
	}
	_ = savepoint.Rollback(ctx)
}

func TestMemoryRuntimeConcurrentBuildAndCanonCommitLoseNoSource(t *testing.T) {
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
	owner, _ := id.New()
	story, _ := id.New()
	tid, _ := id.New()
	var config id.ID
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users(id,username) VALUES($1,$2)`, []any{owner, "memory-concurrent-" + owner.String()}},
		{`INSERT INTO stories(id,owner_id,title) VALUES($1,$2,'memory concurrency')`, []any{story, owner}},
		{`INSERT INTO timelines(id,story_id,name,head_event_seq) VALUES($1,$2,'root',1)`, []any{tid, story}},
	} {
		if _, err = pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err = pool.QueryRow(ctx, `INSERT INTO ai_config_revisions(story_llm_provider,story_llm_model,story_llm_profile,embedding_provider,embedding_model,image_provider,image_model,settings)
 VALUES('openai_compatible','test','test','fake','384','manual','manual-v1','{}') RETURNING id`).Scan(&config); err != nil {
		t.Fatal(err)
	}
	state := pg.NewMemoryRuntimeRepository(pool)
	state.OwnerID = owner
	type answer struct {
		revision int64
		err      error
	}
	answers := make(chan answer, 2)
	start := make(chan struct{})
	eventDone := make(chan error, 1)
	for range 2 {
		go func() {
			<-start
			p, err := state.Ensure(ctx, config, owner.String(), "fake", "384", true)
			answers <- answer{p.Revision, err}
		}()
	}
	go func() {
		<-start
		_, err := pool.Exec(ctx, `INSERT INTO story_events(timeline_id,seq,event_type,payload) VALUES($1,1,'player_action_attempted','{}')`, tid)
		eventDone <- err
	}()
	close(start)
	first, second := <-answers, <-answers
	if err = <-eventDone; err != nil {
		t.Fatal(err)
	}
	if first.err != nil || second.err != nil || first.revision != second.revision {
		t.Fatalf("duplicate builders: %+v %+v", first, second)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM memory_index_jobs WHERE timeline_id=$1 AND source_event_seq=1 AND index_revision=$2`, tid, first.revision).Scan(&count); err != nil || count != 1 {
		t.Fatalf("lost/duplicate committed source: count=%d error=%v", count, err)
	}
}
