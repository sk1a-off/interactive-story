package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	pgadapter "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/application/canon"
	"github.com/local/interactive-ai-story/backend/internal/application/saves"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/savepoint"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/repositories"
)

func TestCanonAppendReplaySnapshotAndStaleHead(t *testing.T) {
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

	ownerRaw, _ := id.New()
	storyRaw, _ := id.New()
	timelineRaw, _ := id.New()
	ownerID := story.UserID(ownerRaw)
	storyID := story.ID(storyRaw)
	timelineID := timeline.ID(timelineRaw)

	if _, err := pool.Exec(ctx, `INSERT INTO users(id,username) VALUES ($1,$2)`, ownerRaw, "test-"+ownerRaw.String()); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	st, err := story.New(storyID, ownerID, "Integration Story", now)
	if err != nil {
		t.Fatal(err)
	}
	tl, err := timeline.New(timelineID, storyID, "Main")
	if err != nil {
		t.Fatal(err)
	}

	uow := pgadapter.NewUnitOfWork(pool)
	if err := uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		if err := repos.Stories().Create(ctx, st); err != nil {
			return err
		}
		return repos.Timelines().Create(ctx, tl)
	}); err != nil {
		t.Fatal(err)
	}

	svc := canon.NewService(uow, func() time.Time { return now })
	pending := []event.PendingEvent{
		{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{"scene_id":"fixture-scene"}`)},
		{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{"beat_id":"fixture-beat"}`)},
	}
	committed, err := svc.AppendSemantic(ctx, timelineID, 0, pending)
	if err != nil {
		t.Fatal(err)
	}
	if len(committed) != 2 || committed[0].Seq != 1 || committed[1].Seq != 2 {
		t.Fatalf("unexpected committed events: %+v", committed)
	}

	if _, err := svc.AppendSemantic(ctx, timelineID, 0, pending[:1]); !errors.Is(err, canon.ErrHeadMoved) {
		t.Fatalf("expected stale head, got %v", err)
	}

	rebuilt, err := svc.Rebuild(ctx, timelineID)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.LastEventSeq != 2 || rebuilt.AppliedEventCount != 2 {
		t.Fatalf("unexpected replay state: %+v", rebuilt)
	}

	snap, err := svc.CreateSnapshot(ctx, timelineID)
	if err != nil {
		t.Fatal(err)
	}
	if err := snap.Verify(); err != nil {
		t.Fatal(err)
	}

	if err := uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		loaded, err := repos.Snapshots().Get(ctx, snap.ID)
		if err != nil {
			return err
		}
		if loaded.StateHash != snap.StateHash || loaded.EventSeq != 2 {
			t.Fatalf("loaded snapshot mismatch: %+v", loaded)
		}
		current, err := repos.Timelines().Get(ctx, timelineID)
		if err != nil {
			return err
		}
		if current.HeadEventSeq != 2 || current.HeadSnapshotID == nil {
			t.Fatalf("timeline head mismatch: %+v", current)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSaveForkExactSequenceAndIsolation(t *testing.T) {
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

	ownerRaw, _ := id.New()
	storyRaw, _ := id.New()
	parentRaw, _ := id.New()
	childRaw, _ := id.New()
	ownerID := story.UserID(ownerRaw)
	storyID := story.ID(storyRaw)
	parentID := timeline.ID(parentRaw)
	childID := timeline.ID(childRaw)
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,username) VALUES ($1,$2)`, ownerRaw, "branch-"+ownerRaw.String()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	st, _ := story.New(storyID, ownerID, "Branching Story", now)
	parent, _ := timeline.New(parentID, storyID, "Main")
	uow := pgadapter.NewUnitOfWork(pool)
	if err := uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		if err := repos.Stories().Create(ctx, st); err != nil {
			return err
		}
		return repos.Timelines().Create(ctx, parent)
	}); err != nil {
		t.Fatal(err)
	}

	chapterRaw, _ := id.New()
	sceneRaw, _ := id.New()
	beatRaw, _ := id.New()
	if _, err := pool.Exec(ctx, `INSERT INTO chapters(id,timeline_id,number,title,status) VALUES($1,$2,1,'Opening','active')`, chapterRaw, parentID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scenes(id,chapter_id,number,status) VALUES($1,$2,1,'active')`, sceneRaw, chapterRaw); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO beats(id,scene_id,position,kind,content,status,is_active) VALUES($1,$2,1,'description','{"text":"base"}','committed',true)`, beatRaw, sceneRaw); err != nil {
		t.Fatal(err)
	}

	canonSvc := canon.NewService(uow, func() time.Time { return now })
	if _, err := canonSvc.AppendSemantic(ctx, parentID, 0, []event.PendingEvent{{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{}`)}, {Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	saveSvc := saves.NewService(uow, func() time.Time { return now }, nil)
	saved, err := saveSvc.Create(ctx, saves.CreateCommand{TimelineID: parentID, Name: "Exact two", Kind: savepoint.KindManual})
	if err != nil {
		t.Fatal(err)
	}
	if saved.EventSeq != 2 {
		t.Fatalf("save seq=%d want=2", saved.EventSeq)
	}
	if _, err = canonSvc.AppendSemantic(ctx, parentID, 2, []event.PendingEvent{{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	child, err := saveSvc.Fork(ctx, saves.ForkCommand{SaveID: saved.ID, TimelineID: childID, Name: "Alternative"})
	if err != nil {
		t.Fatal(err)
	}
	if child.HeadEventSeq != 2 {
		t.Fatalf("child head=%d want=2", child.HeadEventSeq)
	}
	if _, err = canonSvc.AppendSemantic(ctx, childID, 2, []event.PendingEvent{{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{}`)}}); err != nil {
		t.Fatal(err)
	}

	parentState, err := canonSvc.Rebuild(ctx, parentID)
	if err != nil {
		t.Fatal(err)
	}
	childState, err := canonSvc.Rebuild(ctx, childID)
	if err != nil {
		t.Fatal(err)
	}
	if parentState.LastEventSeq != 3 || childState.LastEventSeq != 3 {
		t.Fatalf("bad heads parent=%+v child=%+v", parentState, childState)
	}
	if len(childState.Narrative.Chapters) != 1 || len(childState.Narrative.Scenes) != 1 || len(childState.Narrative.Beats) != 1 {
		t.Fatalf("forked narrative projection missing after rebuild: %+v", childState.Narrative)
	}
	var childChapterID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM chapters WHERE timeline_id=$1`, childID).Scan(&childChapterID); err != nil {
		t.Fatal(err)
	}
	if childChapterID == chapterRaw.String() {
		t.Fatal("fork reused parent chapter UUID")
	}
	var childSceneCount, childBeatCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM scenes s JOIN chapters c ON c.id=s.chapter_id WHERE c.timeline_id=$1`, childID).Scan(&childSceneCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM beats b JOIN scenes s ON s.id=b.scene_id JOIN chapters c ON c.id=s.chapter_id WHERE c.timeline_id=$1`, childID).Scan(&childBeatCount); err != nil {
		t.Fatal(err)
	}
	if childSceneCount != 1 || childBeatCount != 1 {
		t.Fatalf("fork materialization incomplete scenes=%d beats=%d", childSceneCount, childBeatCount)
	}
	var parentOnlyInChild int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM story_events WHERE timeline_id=$1 AND event_type='ParentOnly'`, childID).Scan(&parentOnlyInChild); err != nil {
		t.Fatal(err)
	}
	if parentOnlyInChild != 0 {
		t.Fatal("parent-local event leaked into child")
	}

	// DB must reject a SavePoint that combines a parent snapshot with the child Timeline.
	badID, _ := id.New()
	_, err = pool.Exec(ctx, `INSERT INTO save_points(id,story_id,timeline_id,snapshot_id,event_seq,name,kind) VALUES($1,$2,$3,$4,$5,'invalid','manual')`, badID, storyID, childID, saved.SnapshotID, saved.EventSeq)
	if err == nil {
		t.Fatal("cross-timeline SavePoint unexpectedly accepted")
	}
}
