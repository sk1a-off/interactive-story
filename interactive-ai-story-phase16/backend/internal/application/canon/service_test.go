package canon

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/savepoint"
	"github.com/local/interactive-ai-story/backend/internal/domain/snapshot"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/repositories"
)

type memoryStore struct {
	mu        sync.Mutex
	timelines map[timeline.ID]timeline.Timeline
	events    map[timeline.ID][]event.StoredEvent
	snapshots map[snapshot.ID]snapshot.Snapshot
	narrative map[timeline.ID]narrative.State
}

type memRepos struct{ s *memoryStore }
type memStories struct{}
type memTimelines struct{ s *memoryStore }
type memEvents struct{ s *memoryStore }
type memSnapshots struct{ s *memoryStore }
type memSaves struct{}
type memNarrative struct{ s *memoryStore }

func (memRepos) Stories() repositories.StoryRepository                   { return memStories{} }
func (r memRepos) Timelines() repositories.TimelineRepository            { return memTimelines{r.s} }
func (r memRepos) Events() repositories.EventRepository                  { return memEvents{r.s} }
func (r memRepos) Snapshots() repositories.SnapshotRepository            { return memSnapshots{r.s} }
func (memRepos) SavePoints() repositories.SavePointRepository            { return memSaves{} }
func (r memRepos) Narrative() repositories.NarrativeProjectionRepository { return memNarrative{r.s} }

func (memStories) Create(context.Context, story.Story) error { return nil }
func (memStories) Get(context.Context, story.ID) (story.Story, error) {
	return story.Story{}, errors.New("unused")
}
func (r memTimelines) Create(_ context.Context, v timeline.Timeline) error {
	r.s.timelines[v.ID] = v
	return nil
}
func (r memTimelines) Get(_ context.Context, id timeline.ID) (timeline.Timeline, error) {
	v, ok := r.s.timelines[id]
	if !ok {
		return timeline.Timeline{}, errors.New("not found")
	}
	return v, nil
}
func (r memTimelines) Lock(ctx context.Context, id timeline.ID) (timeline.Timeline, error) {
	return r.Get(ctx, id)
}
func (r memTimelines) ListByStory(_ context.Context, sid story.ID) ([]timeline.Timeline, error) {
	var out []timeline.Timeline
	for _, v := range r.s.timelines {
		if v.StoryID == sid {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r memTimelines) AdvanceHead(_ context.Context, id timeline.ID, seq int64) error {
	v := r.s.timelines[id]
	v.HeadEventSeq = seq
	r.s.timelines[id] = v
	return nil
}
func (r memTimelines) SetHeadSnapshot(_ context.Context, id timeline.ID, sid snapshot.ID) error {
	v := r.s.timelines[id]
	cast := timeline.SnapshotID(sid)
	v.HeadSnapshotID = &cast
	r.s.timelines[id] = v
	return nil
}
func (r memEvents) Append(_ context.Context, id timeline.ID, values []event.StoredEvent) error {
	r.s.events[id] = append(r.s.events[id], values...)
	return nil
}
func (r memEvents) List(_ context.Context, id timeline.ID, after int64) ([]event.StoredEvent, error) {
	var out []event.StoredEvent
	for _, e := range r.s.events[id] {
		if e.Seq > after {
			out = append(out, e)
		}
	}
	return out, nil
}
func (r memSnapshots) Create(_ context.Context, value snapshot.Snapshot) error {
	r.s.snapshots[value.ID] = value
	return nil
}
func (r memSnapshots) Get(_ context.Context, id snapshot.ID) (snapshot.Snapshot, error) {
	v, ok := r.s.snapshots[id]
	if !ok {
		return snapshot.Snapshot{}, errors.New("not found")
	}
	return v, nil
}
func (r memSnapshots) Latest(_ context.Context, tid timeline.ID) (snapshot.Snapshot, error) {
	var latest snapshot.Snapshot
	var found bool
	for _, v := range r.s.snapshots {
		if v.TimelineID == tid && (!found || v.EventSeq > latest.EventSeq) {
			latest, found = v, true
		}
	}
	if !found {
		return snapshot.Snapshot{}, errors.New("not found")
	}
	return latest, nil
}

func (m memNarrative) Export(_ context.Context, tid timeline.ID) (narrative.State, error) {
	if v, ok := m.s.narrative[tid]; ok {
		return v, nil
	}
	return narrative.NewState(id.ID(tid)), nil
}
func (m memNarrative) Materialize(_ context.Context, v narrative.State) error {
	m.s.narrative[timeline.ID(v.TimelineID)] = v
	return nil
}

func (memSaves) Create(context.Context, savepoint.SavePoint) error { return errors.New("unused") }
func (memSaves) Get(context.Context, savepoint.ID) (savepoint.SavePoint, error) {
	return savepoint.SavePoint{}, errors.New("unused")
}
func (memSaves) ListByTimeline(context.Context, timeline.ID) ([]savepoint.SavePoint, error) {
	return nil, errors.New("unused")
}

type memoryUOW struct{ store *memoryStore }

func (u memoryUOW) WithinTransaction(ctx context.Context, fn func(context.Context, repositories.CanonRepositories) error) error {
	u.store.mu.Lock()
	defer u.store.mu.Unlock()
	timelines := make(map[timeline.ID]timeline.Timeline, len(u.store.timelines))
	for k, v := range u.store.timelines {
		timelines[k] = v
	}
	events := make(map[timeline.ID][]event.StoredEvent, len(u.store.events))
	for k, v := range u.store.events {
		events[k] = append([]event.StoredEvent(nil), v...)
	}
	snapshots := make(map[snapshot.ID]snapshot.Snapshot, len(u.store.snapshots))
	for k, v := range u.store.snapshots {
		snapshots[k] = v
	}
	narratives := map[timeline.ID]narrative.State{}
	for k, v := range u.store.narrative {
		narratives[k] = v
	}
	before := &memoryStore{timelines: timelines, events: events, snapshots: snapshots, narrative: narratives}
	if err := fn(ctx, memRepos{u.store}); err != nil {
		u.store.timelines = before.timelines
		u.store.events = before.events
		u.store.snapshots = before.snapshots
		u.store.narrative = before.narrative
		return err
	}
	return nil
}

func newFixture(t *testing.T) (*memoryStore, timeline.ID) {
	t.Helper()
	sidRaw, _ := id.New()
	tidRaw, _ := id.New()
	tl, err := timeline.New(timeline.ID(tidRaw), story.ID(sidRaw), "Main")
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{timelines: map[timeline.ID]timeline.Timeline{tl.ID: tl}, events: map[timeline.ID][]event.StoredEvent{}, snapshots: map[snapshot.ID]snapshot.Snapshot{}, narrative: map[timeline.ID]narrative.State{}}
	return store, tl.ID
}

func TestAppendSemanticRejectsStaleHeadAndCommitsOnce(t *testing.T) {
	store, tid := newFixture(t)
	svc := NewService(memoryUOW{store}, func() time.Time { return time.Unix(10, 0).UTC() })
	pending := []event.PendingEvent{{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{"scene_id":"1"}`)}}
	committed, err := svc.AppendSemantic(context.Background(), tid, 0, pending)
	if err != nil {
		t.Fatal(err)
	}
	if len(committed) != 1 || committed[0].Seq != 1 {
		t.Fatalf("unexpected commit: %+v", committed)
	}
	if _, err := svc.AppendSemantic(context.Background(), tid, 0, pending); !errors.Is(err, ErrHeadMoved) {
		t.Fatalf("expected stale head, got %v", err)
	}
	if got := len(store.events[tid]); got != 1 {
		t.Fatalf("stale append mutated store: %d events", got)
	}
}

func TestSnapshotMatchesReplay(t *testing.T) {
	store, tid := newFixture(t)
	svc := NewService(memoryUOW{store}, time.Now)
	pending := []event.PendingEvent{
		{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{"scene_id":"1"}`)},
		{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{"beat_id":"1"}`)},
	}
	if _, err := svc.AppendSemantic(context.Background(), tid, 0, pending); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := svc.Rebuild(context.Background(), tid)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := svc.CreateSnapshot(context.Background(), tid)
	if err != nil {
		t.Fatal(err)
	}
	if err := snap.Verify(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rebuilt, snap.State) {
		t.Fatalf("snapshot differs from replay: rebuilt=%+v snapshot=%+v", rebuilt, snap.State)
	}
	if store.timelines[tid].HeadSnapshotID == nil {
		t.Fatal("timeline head snapshot not updated")
	}
}

func (memSaves) UpdatePresentation(context.Context, savepoint.ID, *int, bool) error { return nil }

func TestAppendSemanticMaterializesBeatWithSameReducer(t *testing.T) {
	store, tid := newFixture(t)
	svc := NewService(memoryUOW{store}, func() time.Time { return time.Unix(10, 0).UTC() })
	chapter := id.MustParse("00000000-0000-4000-8000-000000000401")
	scene := id.MustParse("00000000-0000-4000-8000-000000000402")
	beat := id.MustParse("00000000-0000-4000-8000-000000000403")
	pending := []event.PendingEvent{
		{Type: "chapter_created", SchemaVersion: 1, Payload: json.RawMessage(`{"chapterId":"00000000-0000-4000-8000-000000000401","number":1,"title":"C","goal":"","tone":"","status":"active"}`)},
		{Type: "scene_started", SchemaVersion: 1, Payload: json.RawMessage(`{"sceneId":"00000000-0000-4000-8000-000000000402","chapterId":"00000000-0000-4000-8000-000000000401","number":1,"goal":"","status":"awaiting_player"}`)},
		{Type: "beat_committed", SchemaVersion: 1, Payload: json.RawMessage(`{"beatId":"00000000-0000-4000-8000-000000000403","sceneId":"00000000-0000-4000-8000-000000000402","position":1,"kind":"mixed","text":"hello","status":"committed"}`)},
	}
	if _, e := svc.AppendSemantic(context.Background(), tid, 0, pending); e != nil {
		t.Fatal(e)
	}
	state := store.narrative[tid]
	if state.Chapters[chapter].ID != chapter || state.Scenes[scene].ID != scene || state.Beats[beat].ID != beat {
		t.Fatal("live Canon projection not materialized from semantic reducer")
	}
	replay, e := svc.Rebuild(context.Background(), tid)
	if e != nil {
		t.Fatal(e)
	}
	if replay.Narrative.Beats[beat].ID != beat {
		t.Fatal("restart replay diverged from live projection")
	}
}
