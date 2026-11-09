package postgres_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/application/memoryindex"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/projection"
	"github.com/local/interactive-ai-story/backend/internal/domain/snapshot"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/memoryindexrepo"
)

func TestMemoryWorkerPublishesVersionsAndInvalidationAtomically(t *testing.T) {
	ctx, tx, story, tid := memoryOutboxFixture(t)
	factID, _ := id.New()
	state := projection.Empty(timeline.ID(tid))
	for seq := int64(0); seq <= 2; seq++ {
		state.LastEventSeq = seq
		value := narrative.Fact{ID: factID, TimelineID: tid, Predicate: "door", Object: json.RawMessage(`"locked"`), Status: narrative.FactActive}
		if seq == 1 {
			value.Object = json.RawMessage(`"open"`)
		}
		if seq == 2 {
			value.Status = narrative.FactInvalidated
		}
		state.Narrative.Facts[factID] = value
		snapID, _ := id.New()
		snap, err := snapshot.New(snapshot.ID(snapID), state, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(snap.State)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO snapshots(id,timeline_id,event_seq,state,state_hash,schema_version,serializer_version) VALUES($1,$2,$3,$4,$5,$6,$7)`, snap.ID, tid, seq, raw, snap.StateHash, snap.SchemaVersion, snap.SerializerVersion); err != nil {
			t.Fatal(err)
		}
	}
	queue := pg.NewMemoryIndexQueue(tx)
	queue.StoryID = story
	store := pg.NewMemoryIndexStore(tx)
	embeddings := fakeai.NewEmbeddingProvider(384)
	profile := repo.Profile{Provider: embeddings.Identity().Provider, Model: embeddings.Identity().Model, Revision: 1}
	now := time.Now().Add(time.Minute)
	worker := memoryindex.Worker{Queue: queue, Store: store, Embeddings: embeddings, Profile: profile, Owner: "test-worker", Now: func() time.Time { return now }}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	badJob, err := queue.Claim(ctx, "test-worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	badDocuments, err := store.Load(ctx, badJob)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Publish(ctx, badJob, profile, badDocuments, nil, now); err == nil {
		t.Fatal("missing vector accepted")
	}
	var retained string
	if err = tx.QueryRow(ctx, `SELECT content FROM memory_embeddings WHERE timeline_id=$1 AND valid_to_event_seq IS NULL`, tid).Scan(&retained); err != nil || !strings.Contains(retained, "locked") {
		t.Fatalf("failed publication damaged previous version: content=%s error=%v", retained, err)
	}
	if err = queue.Retry(ctx, badJob, now, 0, "fixture publication retry"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := worker.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if embeddings.Calls != 2 {
		t.Fatalf("unchanged/removed documents should not be embedded: calls=%d", embeddings.Calls)
	}
	rows, err := tx.Query(ctx, `SELECT content,valid_from_event_seq,valid_to_event_seq FROM memory_embeddings WHERE timeline_id=$1 ORDER BY valid_from_event_seq`, tid)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for rows.Next() {
		var content string
		var from, to int64
		if err = rows.Scan(&content, &from, &to); err != nil {
			t.Fatal(err)
		}
		if from != int64(count) || to != int64(count) {
			t.Fatalf("interval=%d..%d", from, to)
		}
		if count == 0 && !strings.Contains(content, "locked") {
			t.Fatal("initial snapshot indexed future state")
		}
		if count == 1 && !strings.Contains(content, "open") {
			t.Fatal("replacement missing")
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil || count != 2 {
		t.Fatalf("versions=%d error=%v", count, err)
	}
	var published, completed int64
	if err = tx.QueryRow(ctx, `SELECT published_event_seq FROM memory_index_publication_state WHERE timeline_id=$1`, tid).Scan(&published); err != nil || published != 2 {
		t.Fatalf("published=%d error=%v", published, err)
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM memory_index_jobs WHERE timeline_id=$1 AND status='completed'`, tid).Scan(&completed); err != nil || completed != 3 {
		t.Fatalf("completed=%d error=%v", completed, err)
	}
}

func TestMemorySourceReplaysExactEventAndRejectsMissingHistory(t *testing.T) {
	ctx, tx, story, tid := memoryOutboxFixture(t)
	factID, _ := id.New()
	for seq, value := range []string{"locked", "open"} {
		payload, _ := json.Marshal(map[string]any{"commandType": "upsert_fact", "after": map[string]any{"ID": factID, "SubjectType": "world", "Predicate": "door", "Object": value}})
		if _, err := tx.Exec(ctx, `INSERT INTO story_events(timeline_id,seq,event_type,payload) VALUES($1,$2,'director_exact_edit_applied',$3)`, tid, seq+1, payload); err != nil {
			t.Fatal(err)
		}
	}
	queue := pg.NewMemoryIndexQueue(tx)
	queue.StoryID = story
	job, err := queue.Claim(ctx, "test", time.Now().Add(time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store := pg.NewMemoryIndexStore(tx)
	documents, err := store.Load(ctx, job)
	if err != nil || len(documents) != 1 || !strings.Contains(documents[0].Content, "locked") || strings.Contains(documents[0].Content, "open") {
		t.Fatalf("documents=%+v error=%v", documents, err)
	}
	wrong := job
	wrong.SourceEventSeq = 2
	if _, err = store.Load(ctx, wrong); err == nil {
		t.Fatal("tampered job accepted")
	}
	// A gap must fail closed rather than silently stamp current state at seq 2.
	other, _ := id.New()
	if _, err = tx.Exec(ctx, `INSERT INTO timelines(id,story_id,name,head_event_seq) VALUES($1,$2,'gap',2)`, other, story); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO story_events(timeline_id,seq,event_type,payload) VALUES($1,2,'player_action_attempted','{}')`, other); err != nil {
		t.Fatal(err)
	}
	// The first timeline is still running, so the independent branch is eligible.
	gap, err := queue.Claim(ctx, "other", time.Now().Add(time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Load(ctx, gap); err == nil {
		t.Fatal("missing event history accepted")
	}
}

func TestMemoryPublicationRejectsStaleLeaseAndRetriesIdempotently(t *testing.T) {
	ctx, tx, story, tid := memoryOutboxFixture(t)
	if _, err := tx.Exec(ctx, `INSERT INTO story_events(timeline_id,seq,event_type,payload) VALUES($1,1,'player_action_attempted','{}')`, tid); err != nil {
		t.Fatal(err)
	}
	queue := pg.NewMemoryIndexQueue(tx)
	queue.StoryID = story
	now := time.Now().Add(time.Minute)
	old, err := queue.Claim(ctx, "same-owner", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	current, err := queue.Claim(ctx, "same-owner", now.Add(2*time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store := pg.NewMemoryIndexStore(tx)
	profile := repo.Profile{Provider: "test", Model: "384-test", Revision: 1}
	if err = store.Publish(ctx, old, profile, nil, nil, now.Add(2*time.Second)); !errors.Is(err, repo.ErrLeaseLost) {
		t.Fatalf("stale publisher error=%v", err)
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM memory_index_publications WHERE timeline_id=$1`, tid).Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale publication left data count=%d error=%v", count, err)
	}
	if err = store.Publish(ctx, current, profile, nil, nil, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = store.Publish(ctx, current, profile, nil, nil, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM memory_index_publications WHERE timeline_id=$1`, tid).Scan(&count); err != nil || count != 1 {
		t.Fatalf("idempotent publication count=%d error=%v", count, err)
	}
}

func TestHistoricalBeatUsesEventPayloadAndSurvivesLaterCheckpoint(t *testing.T) {
	ctx, tx, story, tid := memoryOutboxFixture(t)
	chapter, _ := id.New()
	scene, _ := id.New()
	beat, _ := id.New()
	for _, query := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO chapters(id,timeline_id,number,title,status) VALUES($1,$2,1,'fixture','active')`, []any{chapter, tid}},
		{`INSERT INTO scenes(id,chapter_id,number,status) VALUES($1,$2,1,'active')`, []any{scene, chapter}},
		{`INSERT INTO beats(id,scene_id,position,kind,content,status,is_active) VALUES($1,$2,1,'description','{"text":"future materialized text"}','committed',true)`, []any{beat, scene}},
	} {
		if _, err := tx.Exec(ctx, query.sql, query.args...); err != nil {
			t.Fatal(err)
		}
	}
	payload, _ := json.Marshal(map[string]any{"beatId": beat, "sceneId": scene, "position": 1, "kind": "description", "text": "original historical text", "status": "committed"})
	if _, err := tx.Exec(ctx, `INSERT INTO story_events(timeline_id,seq,event_type,payload) VALUES($1,1,'beat_committed',$2),($1,2,'player_action_attempted','{}')`, tid, payload); err != nil {
		t.Fatal(err)
	}
	queue := pg.NewMemoryIndexQueue(tx)
	queue.StoryID = story
	embeddings := fakeai.NewEmbeddingProvider(384)
	worker := memoryindex.Worker{Queue: queue, Store: pg.NewMemoryIndexStore(tx), Embeddings: embeddings, Profile: repo.Profile{Provider: embeddings.Identity().Provider, Model: embeddings.Identity().Model, Revision: 1}, Owner: "worker", Now: func() time.Time { return time.Now().Add(time.Minute) }}
	for range 2 {
		if err := worker.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var content, class string
	var until *int64
	if err := tx.QueryRow(ctx, `SELECT content,document_class,valid_to_event_seq FROM memory_embeddings WHERE timeline_id=$1`, tid).Scan(&content, &class, &until); err != nil {
		t.Fatal(err)
	}
	if content != "original historical text" || class != "historical_beat" || until != nil || embeddings.Calls != 1 {
		t.Fatalf("content=%s class=%s until=%v embedding calls=%d", content, class, until, embeddings.Calls)
	}
}
