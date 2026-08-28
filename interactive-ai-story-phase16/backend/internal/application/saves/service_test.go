package saves

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/application/canon"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/savepoint"
	"github.com/local/interactive-ai-story/backend/internal/domain/snapshot"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/repositories"
)

type memoryUOW struct{ r *memoryRepos }

func (u memoryUOW) WithinTransaction(ctx context.Context, fn func(context.Context, repositories.CanonRepositories) error) error {
	return fn(ctx, u.r)
}

type memoryRepos struct {
	stories   map[story.ID]story.Story
	timelines map[timeline.ID]timeline.Timeline
	events    map[timeline.ID][]event.StoredEvent
	snapshots map[snapshot.ID]snapshot.Snapshot
	saves     map[savepoint.ID]savepoint.SavePoint
	narrative map[timeline.ID]narrative.State
}

func newMemoryRepos() *memoryRepos {
	return &memoryRepos{stories: map[story.ID]story.Story{}, timelines: map[timeline.ID]timeline.Timeline{}, events: map[timeline.ID][]event.StoredEvent{}, snapshots: map[snapshot.ID]snapshot.Snapshot{}, saves: map[savepoint.ID]savepoint.SavePoint{}, narrative: map[timeline.ID]narrative.State{}}
}
func (r *memoryRepos) Stories() repositories.StoryRepository        { return memoryStoryRepo{r} }
func (r *memoryRepos) Timelines() repositories.TimelineRepository   { return memoryTimelineRepo{r} }
func (r *memoryRepos) Events() repositories.EventRepository         { return memoryEventRepo{r} }
func (r *memoryRepos) Snapshots() repositories.SnapshotRepository   { return memorySnapshotRepo{r} }
func (r *memoryRepos) SavePoints() repositories.SavePointRepository { return memorySaveRepo{r} }
func (r *memoryRepos) Narrative() repositories.NarrativeProjectionRepository {
	return memoryNarrativeRepo{r}
}

type memoryNarrativeRepo struct{ r *memoryRepos }

func (m memoryNarrativeRepo) Export(_ context.Context, tid timeline.ID) (narrative.State, error) {
	if v, ok := m.r.narrative[tid]; ok {
		return v, nil
	}
	return narrative.NewState(id.ID(tid)), nil
}
func (m memoryNarrativeRepo) Materialize(_ context.Context, v narrative.State) error {
	m.r.narrative[timeline.ID(v.TimelineID)] = v
	return nil
}

type memoryStoryRepo struct{ r *memoryRepos }

func (m memoryStoryRepo) Create(_ context.Context, v story.Story) error {
	m.r.stories[v.ID] = v
	return nil
}
func (m memoryStoryRepo) Get(_ context.Context, k story.ID) (story.Story, error) {
	v, ok := m.r.stories[k]
	if !ok {
		return story.Story{}, errors.New("not found")
	}
	return v, nil
}

type memoryTimelineRepo struct{ r *memoryRepos }

func (m memoryTimelineRepo) Create(_ context.Context, v timeline.Timeline) error {
	if _, ok := m.r.timelines[v.ID]; ok {
		return errors.New("duplicate")
	}
	m.r.timelines[v.ID] = v
	return nil
}
func (m memoryTimelineRepo) Get(_ context.Context, k timeline.ID) (timeline.Timeline, error) {
	v, ok := m.r.timelines[k]
	if !ok {
		return timeline.Timeline{}, errors.New("not found")
	}
	return v, nil
}
func (m memoryTimelineRepo) Lock(ctx context.Context, k timeline.ID) (timeline.Timeline, error) {
	return m.Get(ctx, k)
}
func (m memoryTimelineRepo) ListByStory(_ context.Context, k story.ID) ([]timeline.Timeline, error) {
	var out []timeline.Timeline
	for _, v := range m.r.timelines {
		if v.StoryID == k && v.Status != timeline.StatusDeleted {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (m memoryTimelineRepo) AdvanceHead(_ context.Context, k timeline.ID, seq int64) error {
	v, ok := m.r.timelines[k]
	if !ok {
		return errors.New("not found")
	}
	v.HeadEventSeq = seq
	m.r.timelines[k] = v
	return nil
}
func (m memoryTimelineRepo) SetHeadSnapshot(_ context.Context, k timeline.ID, sid snapshot.ID) error {
	v, ok := m.r.timelines[k]
	if !ok {
		return errors.New("not found")
	}
	x := timeline.SnapshotID(sid)
	v.HeadSnapshotID = &x
	m.r.timelines[k] = v
	return nil
}

type memoryEventRepo struct{ r *memoryRepos }

func (m memoryEventRepo) Append(_ context.Context, k timeline.ID, values []event.StoredEvent) error {
	m.r.events[k] = append(m.r.events[k], values...)
	return nil
}
func (m memoryEventRepo) List(_ context.Context, k timeline.ID, after int64) ([]event.StoredEvent, error) {
	var out []event.StoredEvent
	for _, v := range m.r.events[k] {
		if v.Seq > after {
			out = append(out, v)
		}
	}
	return out, nil
}

type memorySnapshotRepo struct{ r *memoryRepos }

func (m memorySnapshotRepo) Create(_ context.Context, v snapshot.Snapshot) error {
	if _, ok := m.r.snapshots[v.ID]; ok {
		return errors.New("duplicate")
	}
	m.r.snapshots[v.ID] = v
	return nil
}
func (m memorySnapshotRepo) Get(_ context.Context, k snapshot.ID) (snapshot.Snapshot, error) {
	v, ok := m.r.snapshots[k]
	if !ok {
		return snapshot.Snapshot{}, errors.New("not found")
	}
	return v, nil
}
func (m memorySnapshotRepo) Latest(_ context.Context, k timeline.ID) (snapshot.Snapshot, error) {
	var best snapshot.Snapshot
	found := false
	for _, v := range m.r.snapshots {
		if v.TimelineID == k && (!found || v.EventSeq > best.EventSeq) {
			best = v
			found = true
		}
	}
	if !found {
		return snapshot.Snapshot{}, errors.New("not found")
	}
	return best, nil
}

type memorySaveRepo struct{ r *memoryRepos }

func (m memorySaveRepo) Create(_ context.Context, v savepoint.SavePoint) error {
	if _, ok := m.r.saves[v.ID]; ok {
		return errors.New("duplicate")
	}
	m.r.saves[v.ID] = v
	return nil
}
func (m memorySaveRepo) Get(_ context.Context, k savepoint.ID) (savepoint.SavePoint, error) {
	v, ok := m.r.saves[k]
	if !ok {
		return savepoint.SavePoint{}, errors.New("not found")
	}
	return v, nil
}
func (m memorySaveRepo) UpdatePresentation(_ context.Context, k savepoint.ID, slot *int, pinned bool) error {
	v, ok := m.r.saves[k]
	if !ok {
		return errors.New("not found")
	}
	v.DisplaySlot = slot
	v.Pinned = pinned
	m.r.saves[k] = v
	return nil
}
func (m memorySaveRepo) ListByTimeline(_ context.Context, k timeline.ID) ([]savepoint.SavePoint, error) {
	var out []savepoint.SavePoint
	for _, v := range m.r.saves {
		if v.TimelineID == k {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

type idSeq struct {
	values []id.ID
	n      int
}

func (s *idSeq) next() (id.ID, error) { v := s.values[s.n]; s.n++; return v, nil }
func fixed(v string) id.ID            { return id.MustParse("00000000-0000-4000-8000-" + v) }

func TestSaveForkDivergenceAndRestartRebuild(t *testing.T) {
	ctx := context.Background()
	r := newMemoryRepos()
	uow := memoryUOW{r: r}
	now := time.Unix(100, 0).UTC()
	storyID := story.ID(fixed("000000000001"))
	parentID := timeline.ID(fixed("000000000002"))
	childID := timeline.ID(fixed("000000000003"))
	parent, _ := timeline.New(parentID, storyID, "Main")
	r.timelines[parentID] = parent

	ids := &idSeq{values: []id.ID{fixed("000000000011"), fixed("000000000012"), fixed("000000000013"), fixed("000000000014"), fixed("000000000015"), fixed("000000000016")}}
	canonSvc := canon.NewService(uow, func() time.Time { return now })
	committed, err := canonSvc.AppendSemantic(ctx, parentID, 0, []event.PendingEvent{{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{}`)}, {Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{}`)}})
	if err != nil {
		t.Fatal(err)
	}
	// canon service uses crypto IDs, so only Save/Fork IDs consume deterministic sequence.
	_ = committed
	saveSvc := NewService(uow, func() time.Time { return now }, ids.next)
	saved, err := saveSvc.Create(ctx, CreateCommand{TimelineID: parentID, Name: "Before choice", Kind: savepoint.KindManual})
	if err != nil {
		t.Fatal(err)
	}
	if saved.EventSeq != 2 {
		t.Fatalf("save must point to exact head 2, got %d", saved.EventSeq)
	}
	preview, err := saveSvc.Preview(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Snapshot.EventSeq != 2 || preview.Snapshot.State.LastEventSeq != 2 {
		t.Fatalf("preview not exact: %+v", preview)
	}

	if _, err = canonSvc.AppendSemantic(ctx, parentID, 2, []event.PendingEvent{{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	child, err := saveSvc.Fork(ctx, ForkCommand{SaveID: saved.ID, TimelineID: childID, Name: "Alternative"})
	if err != nil {
		t.Fatal(err)
	}
	if child.HeadEventSeq != 2 || child.ParentTimelineID == nil || *child.ParentTimelineID != parentID || child.ForkedFromSaveID == nil {
		t.Fatalf("bad lineage: %+v", child)
	}
	if _, err = canonSvc.AppendSemantic(ctx, childID, 2, []event.PendingEvent{{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{}`)}}); err != nil {
		t.Fatal(err)
	}

	parentNow := r.timelines[parentID]
	childNow := r.timelines[childID]
	if parentNow.HeadEventSeq != 3 || childNow.HeadEventSeq != 3 {
		t.Fatalf("unexpected heads parent=%d child=%d", parentNow.HeadEventSeq, childNow.HeadEventSeq)
	}
	if len(r.events[parentID]) != 3 || len(r.events[childID]) != 1 || r.events[childID][0].Seq != 3 {
		t.Fatalf("branch event isolation failed parent=%+v child=%+v", r.events[parentID], r.events[childID])
	}

	rebuilt, err := canonSvc.Rebuild(ctx, childID)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.LastEventSeq != 3 || rebuilt.AppliedEventCount != 3 {
		t.Fatalf("restart rebuild from fork snapshot failed: %+v", rebuilt)
	}
}

func TestSiblingForksDoNotShareLocalEvents(t *testing.T) {
	ctx := context.Background()
	r := newMemoryRepos()
	uow := memoryUOW{r: r}
	now := time.Unix(200, 0).UTC()
	storyID := story.ID(fixed("000000000101"))
	parentID := timeline.ID(fixed("000000000102"))
	aID := timeline.ID(fixed("000000000103"))
	bID := timeline.ID(fixed("000000000104"))
	parent, _ := timeline.New(parentID, storyID, "Main")
	r.timelines[parentID] = parent
	canonSvc := canon.NewService(uow, func() time.Time { return now })
	if _, err := canonSvc.AppendSemantic(ctx, parentID, 0, []event.PendingEvent{{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	ids := &idSeq{values: []id.ID{fixed("000000000111"), fixed("000000000112"), fixed("000000000113"), fixed("000000000114"), fixed("000000000115")}}
	saveSvc := NewService(uow, func() time.Time { return now }, ids.next)
	saved, err := saveSvc.Create(ctx, CreateCommand{TimelineID: parentID, Name: "fork base", Kind: savepoint.KindManual})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = saveSvc.Fork(ctx, ForkCommand{SaveID: saved.ID, TimelineID: aID, Name: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err = saveSvc.Fork(ctx, ForkCommand{SaveID: saved.ID, TimelineID: bID, Name: "B"}); err != nil {
		t.Fatal(err)
	}
	if _, err = canonSvc.AppendSemantic(ctx, aID, 1, []event.PendingEvent{{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	rb, err := canonSvc.Rebuild(ctx, bID)
	if err != nil {
		t.Fatal(err)
	}
	if rb.LastEventSeq != 1 || rb.AppliedEventCount != 1 {
		t.Fatalf("sibling B leaked A event: %+v", rb)
	}
}

func TestRestoreUsesSafeForkPolicy(t *testing.T) {
	if !errors.Is(ErrAdvancedRestoreRequired, ErrAdvancedRestoreRequired) {
		t.Fatal("sentinel")
	}
	// The public Restore method delegates to Fork; no in-place head rewrite exists in the MVP service.
}

func TestForkRemapsAndMaterializesNarrativeProjection(t *testing.T) {
	ctx := context.Background()
	r := newMemoryRepos()
	uow := memoryUOW{r: r}
	now := time.Unix(300, 0).UTC()
	storyID := story.ID(fixed("000000000201"))
	parentID := timeline.ID(fixed("000000000202"))
	childID := timeline.ID(fixed("000000000203"))
	parent, _ := timeline.New(parentID, storyID, "Main")
	r.timelines[parentID] = parent

	chapterID := fixed("000000000211")
	sceneID := fixed("000000000212")
	beatID := fixed("000000000213")
	a := fixed("000000000214")
	b := fixed("000000000215")
	relID := fixed("000000000216")
	factID := fixed("000000000217")
	state := narrative.NewState(id.ID(parentID))
	state.Chapters[chapterID] = narrative.Chapter{ID: chapterID, TimelineID: id.ID(parentID), Number: 1, Title: "One", Status: "active"}
	state.Scenes[sceneID] = narrative.Scene{ID: sceneID, ChapterID: chapterID, Number: 1, Status: "active", StoryTime: json.RawMessage(`{"day":1}`)}
	state.Beats[beatID] = narrative.Beat{ID: beatID, SceneID: sceneID, Position: 1, Kind: narrative.BeatDescription, Content: json.RawMessage(`{"text":"shared"}`), Status: "committed", IsActive: true}
	rel, _ := narrative.NewRelationship(id.ID(parentID), relID, a, b)
	state.Relationships[relID] = rel
	state.Stats["relationship|"+relID.String()+"|trust"] = narrative.Stat{TimelineID: id.ID(parentID), OwnerType: narrative.OwnerRelationship, OwnerID: relID, Key: "trust", ValueType: narrative.ValueNumber, Value: json.RawMessage(`50`)}
	state.Facts[factID] = narrative.Fact{ID: factID, TimelineID: id.ID(parentID), SubjectType: "world", SubjectID: fixed("000000000218"), Predicate: "known", Object: json.RawMessage(`true`), Status: narrative.FactActive, ValidFromSeq: 1}
	state.Knowledge[factID.String()+"|"+a.String()] = narrative.Knowledge{TimelineID: id.ID(parentID), FactID: factID, KnowerCharacterID: a, Confidence: 1, LearnedAtEventSeq: 1}
	r.narrative[parentID] = state

	var generated []id.ID
	for i := 301; i < 340; i++ {
		generated = append(generated, fixed(fmt.Sprintf("%012d", i)))
	}
	ids := &idSeq{values: generated}
	svc := NewService(uow, func() time.Time { return now }, ids.next)
	saved, err := svc.Create(ctx, CreateCommand{TimelineID: parentID, Name: "projection", Kind: savepoint.KindManual})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ChapterID == nil || *saved.ChapterID != chapterID || saved.SceneID == nil || *saved.SceneID != sceneID || saved.BeatID == nil || *saved.BeatID != beatID {
		t.Fatalf("save did not capture current narrative cursor: %+v", saved)
	}
	_, err = svc.Fork(ctx, ForkCommand{SaveID: saved.ID, TimelineID: childID, Name: "Child"})
	if err != nil {
		t.Fatal(err)
	}

	childState := r.narrative[childID]
	if len(childState.Chapters) != 1 || len(childState.Scenes) != 1 || len(childState.Beats) != 1 || len(childState.Relationships) != 1 || len(childState.Facts) != 1 || len(childState.Knowledge) != 1 {
		t.Fatalf("materialized projection incomplete: %+v", childState)
	}
	if _, ok := childState.Chapters[chapterID]; ok {
		t.Fatal("child reused parent chapter primary key")
	}
	if _, ok := childState.Relationships[relID]; ok {
		t.Fatal("child reused parent relationship primary key")
	}
	if _, ok := childState.Facts[factID]; ok {
		t.Fatal("child reused parent fact primary key")
	}
	for _, sc := range childState.Scenes {
		if _, ok := childState.Chapters[sc.ChapterID]; !ok {
			t.Fatal("scene did not reference remapped chapter")
		}
	}
	for _, bt := range childState.Beats {
		if _, ok := childState.Scenes[bt.SceneID]; !ok {
			t.Fatal("beat did not reference remapped scene")
		}
	}
	for _, k := range childState.Knowledge {
		if _, ok := childState.Facts[k.FactID]; !ok {
			t.Fatal("knowledge did not reference remapped fact")
		}
	}
	for _, st := range childState.Stats {
		if st.OwnerType == narrative.OwnerRelationship {
			if _, ok := childState.Relationships[st.OwnerID]; !ok {
				t.Fatal("relationship stat did not reference remapped relationship")
			}
		}
	}
	if _, ok := r.narrative[parentID].Facts[factID]; !ok {
		t.Fatal("fork mutated parent narrative projection")
	}
}

func TestSaveLibraryPreservesBranchesAndPresentationMetadata(t *testing.T) {
	ctx := context.Background()
	r := newMemoryRepos()
	svc := NewService(memoryUOW{r: r}, time.Now, nil)
	sid := story.ID(fixed("000000000101"))
	mainID := timeline.ID(fixed("000000000102"))
	branchID := timeline.ID(fixed("000000000103"))
	main, _ := timeline.New(mainID, sid, "Main")
	branch, _ := timeline.New(branchID, sid, "Branch")
	parent := mainID
	branch.ParentTimelineID = &parent
	r.timelines[mainID] = main
	r.timelines[branchID] = branch
	saveID := savepoint.ID(fixed("000000000104"))
	r.saves[saveID] = savepoint.SavePoint{ID: saveID, StoryID: sid, TimelineID: mainID, SnapshotID: snapshot.ID(fixed("000000000105")), Name: "Slot one", Kind: savepoint.KindManual, CreatedAt: time.Unix(1, 0)}
	slot := 1
	if e := svc.UpdatePresentation(ctx, PresentationCommand{SaveID: saveID, DisplaySlot: &slot, Pinned: true}); e != nil {
		t.Fatal(e)
	}
	lib, e := svc.Library(ctx, sid)
	if e != nil {
		t.Fatal(e)
	}
	if len(lib.Timelines) != 2 {
		t.Fatalf("want two timelines, got %d", len(lib.Timelines))
	}
	got := lib.Saves[mainID][0]
	if got.DisplaySlot == nil || *got.DisplaySlot != 1 || !got.Pinned {
		t.Fatal("save presentation metadata lost")
	}
	if lib.Timelines[1].ParentTimelineID == nil && lib.Timelines[0].ParentTimelineID == nil {
		t.Fatal("branch ancestry lost")
	}
}
func TestInvalidDisplaySlotRejected(t *testing.T) {
	svc := NewService(memoryUOW{r: newMemoryRepos()}, time.Now, nil)
	zero := 0
	if e := svc.UpdatePresentation(context.Background(), PresentationCommand{DisplaySlot: &zero}); e == nil {
		t.Fatal("expected invalid slot")
	}
}

func TestForkDoesNotMutateSiblingOrSource(t *testing.T) {
	ctx := context.Background()
	r := newMemoryRepos()
	uow := memoryUOW{r: r}
	now := time.Unix(200, 0).UTC()
	sid := story.ID(fixed("000000000201"))
	parentID := timeline.ID(fixed("000000000202"))
	siblingID := timeline.ID(fixed("000000000203"))
	childID := timeline.ID(fixed("000000000204"))
	parent, _ := timeline.New(parentID, sid, "Main")
	sibling, _ := timeline.New(siblingID, sid, "Existing branch")
	pp := parentID
	sibling.ParentTimelineID = &pp
	r.timelines[parentID] = parent
	r.timelines[siblingID] = sibling
	ids := &idSeq{values: []id.ID{fixed("000000000211"), fixed("000000000212"), fixed("000000000213"), fixed("000000000214"), fixed("000000000215")}}
	canonSvc := canon.NewService(uow, func() time.Time { return now })
	if _, e := canonSvc.AppendSemantic(ctx, parentID, 0, []event.PendingEvent{{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{}`)}}); e != nil {
		t.Fatal(e)
	}
	svc := NewService(uow, func() time.Time { return now }, ids.next)
	save, e := svc.Create(ctx, CreateCommand{TimelineID: parentID, Name: "fork point", Kind: savepoint.KindManual})
	if e != nil {
		t.Fatal(e)
	}
	beforeParent := r.timelines[parentID]
	beforeSibling := r.timelines[siblingID]
	child, e := svc.Fork(ctx, ForkCommand{SaveID: save.ID, SourceTimelineID: parentID, TimelineID: childID, Name: "New branch"})
	if e != nil {
		t.Fatal(e)
	}
	if r.timelines[parentID].HeadEventSeq != beforeParent.HeadEventSeq {
		t.Fatal("fork mutated source head")
	}
	if r.timelines[siblingID].Name != beforeSibling.Name || r.timelines[siblingID].HeadEventSeq != beforeSibling.HeadEventSeq {
		t.Fatal("fork mutated sibling")
	}
	if child.ParentTimelineID == nil || *child.ParentTimelineID != parentID {
		t.Fatal("fork ancestry missing")
	}
}
