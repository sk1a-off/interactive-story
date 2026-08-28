package saves

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/projection"
	"github.com/local/interactive-ai-story/backend/internal/domain/savepoint"
	"github.com/local/interactive-ai-story/backend/internal/domain/snapshot"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/repositories"
	"github.com/local/interactive-ai-story/backend/internal/ports/tx"
)

var (
	ErrInvalidForkName         = errors.New("fork timeline name is required")
	ErrAdvancedRestoreRequired = errors.New("destructive restore is an advanced workflow; use safe fork")
)

type IDGenerator func() (id.ID, error)

type Service struct {
	uow   tx.UnitOfWork
	now   func() time.Time
	newID IDGenerator
}

func NewService(uow tx.UnitOfWork, now func() time.Time, newID IDGenerator) Service {
	if newID == nil {
		newID = id.New
	}
	return Service{uow: uow, now: now, newID: newID}
}

type CreateCommand struct {
	TimelineID timeline.ID
	Name       string
	Note       string
	Kind       savepoint.Kind
	Pinned     bool
}

func (s Service) Create(ctx context.Context, cmd CreateCommand) (savepoint.SavePoint, error) {
	var created savepoint.SavePoint
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		current, err := repos.Timelines().Lock(ctx, cmd.TimelineID)
		if err != nil {
			return err
		}
		snap, err := s.ensureSnapshotAtHead(ctx, repos, current)
		if err != nil {
			return err
		}
		raw, err := s.newID()
		if err != nil {
			return err
		}
		created, err = savepoint.New(savepoint.ID(raw), current.StoryID, current.ID, snap, cmd.Name, cmd.Note, cmd.Kind, cmd.Pinned, s.now())
		if err != nil {
			return err
		}
		created.ChapterID, created.SceneID, created.BeatID = currentCursor(snap.State.Narrative)
		return repos.SavePoints().Create(ctx, created)
	})
	return created, err
}

type Preview struct {
	Save     savepoint.SavePoint
	Snapshot snapshot.Snapshot
}

func (s Service) Preview(ctx context.Context, saveID savepoint.ID) (Preview, error) {
	var out Preview
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		value, err := repos.SavePoints().Get(ctx, saveID)
		if err != nil {
			return err
		}
		snap, err := repos.Snapshots().Get(ctx, value.SnapshotID)
		if err != nil {
			return err
		}
		if snap.TimelineID != value.TimelineID || snap.EventSeq != value.EventSeq {
			return savepoint.ErrInvalidSavePoint
		}
		out = Preview{Save: value, Snapshot: snap}
		return nil
	})
	return out, err
}

type ForkCommand struct {
	SaveID           savepoint.ID
	SourceTimelineID timeline.ID
	TimelineID       timeline.ID
	Name             string
}

func (s Service) Fork(ctx context.Context, cmd ForkCommand) (timeline.Timeline, error) {
	if strings.TrimSpace(cmd.Name) == "" {
		return timeline.Timeline{}, ErrInvalidForkName
	}
	var child timeline.Timeline
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		save, err := repos.SavePoints().Get(ctx, cmd.SaveID)
		if err != nil {
			return err
		}
		if !id.ID(cmd.SourceTimelineID).IsZero() && save.TimelineID != cmd.SourceTimelineID {
			return savepoint.ErrInvalidSavePoint
		}
		source, err := repos.Timelines().Get(ctx, save.TimelineID)
		if err != nil {
			return err
		}
		if source.StoryID != save.StoryID {
			return savepoint.ErrInvalidSavePoint
		}
		sourceSnap, err := repos.Snapshots().Get(ctx, save.SnapshotID)
		if err != nil {
			return err
		}
		if sourceSnap.TimelineID != save.TimelineID || sourceSnap.EventSeq != save.EventSeq {
			return savepoint.ErrInvalidSavePoint
		}

		child, err = timeline.New(cmd.TimelineID, save.StoryID, cmd.Name)
		if err != nil {
			return err
		}
		parentID := source.ID
		forkSaveID := timeline.SavePointID(save.ID)
		child.ParentTimelineID = &parentID
		child.ForkedFromSaveID = &forkSaveID
		child.HeadEventSeq = save.EventSeq
		if err := repos.Timelines().Create(ctx, child); err != nil {
			return err
		}

		snapRaw, err := s.newID()
		if err != nil {
			return err
		}
		childSnap, err := snapshot.CloneForTimelineWithIDs(snapshot.ID(snapRaw), sourceSnap, child.ID, s.now(), func() (id.ID, error) { return s.newID() })
		if err != nil {
			return err
		}
		if err := repos.Snapshots().Create(ctx, childSnap); err != nil {
			return err
		}
		if err := repos.Narrative().Materialize(ctx, childSnap.State.Narrative); err != nil {
			return err
		}
		if err := repos.Timelines().SetHeadSnapshot(ctx, child.ID, childSnap.ID); err != nil {
			return err
		}
		sid := timeline.SnapshotID(childSnap.ID)
		child.HeadSnapshotID = &sid
		return nil
	})
	return child, err
}

// Restore follows the current MVP policy: loading an old save is a safe fork.
// Destructive in-place rewind is intentionally not hidden behind this method.
func (s Service) Restore(ctx context.Context, saveID savepoint.ID, newTimelineID timeline.ID, newName string) (timeline.Timeline, error) {
	return s.Fork(ctx, ForkCommand{SaveID: saveID, TimelineID: newTimelineID, Name: newName})
}

func (s Service) ListTimeline(ctx context.Context, storyID story.ID) ([]timeline.Timeline, error) {
	var out []timeline.Timeline
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		var err error
		out, err = repos.Timelines().ListByStory(ctx, storyID)
		return err
	})
	return out, err
}

func (s Service) ListSaves(ctx context.Context, timelineID timeline.ID) ([]savepoint.SavePoint, error) {
	var out []savepoint.SavePoint
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		var err error
		out, err = repos.SavePoints().ListByTimeline(ctx, timelineID)
		return err
	})
	return out, err
}

type PresentationCommand struct {
	SaveID      savepoint.ID
	DisplaySlot *int
	Pinned      bool
}

func (s Service) UpdatePresentation(ctx context.Context, cmd PresentationCommand) error {
	if cmd.DisplaySlot != nil && *cmd.DisplaySlot < 1 {
		return savepoint.ErrInvalidSavePoint
	}
	return s.uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		if _, err := repos.SavePoints().Get(ctx, cmd.SaveID); err != nil {
			return err
		}
		return repos.SavePoints().UpdatePresentation(ctx, cmd.SaveID, cmd.DisplaySlot, cmd.Pinned)
	})
}

type Library struct {
	Timelines []timeline.Timeline
	Saves     map[timeline.ID][]savepoint.SavePoint
}

func (s Service) Library(ctx context.Context, storyID story.ID) (Library, error) {
	out := Library{Saves: map[timeline.ID][]savepoint.SavePoint{}}
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		var err error
		out.Timelines, err = repos.Timelines().ListByStory(ctx, storyID)
		if err != nil {
			return err
		}
		for _, tl := range out.Timelines {
			values, e := repos.SavePoints().ListByTimeline(ctx, tl.ID)
			if e != nil {
				return e
			}
			out.Saves[tl.ID] = values
		}
		return nil
	})
	return out, err
}

func (s Service) ensureSnapshotAtHead(ctx context.Context, repos repositories.CanonRepositories, current timeline.Timeline) (snapshot.Snapshot, error) {
	if current.HeadSnapshotID != nil {
		existing, err := repos.Snapshots().Get(ctx, snapshot.ID(*current.HeadSnapshotID))
		if err != nil {
			return snapshot.Snapshot{}, err
		}
		if existing.TimelineID == current.ID && existing.EventSeq == current.HeadEventSeq {
			return existing, nil
		}
	}

	state := projection.Empty(current.ID)
	if current.HeadSnapshotID != nil {
		base, err := repos.Snapshots().Get(ctx, snapshot.ID(*current.HeadSnapshotID))
		if err != nil {
			return snapshot.Snapshot{}, err
		}
		state = base.State
	}
	events, err := repos.Events().List(ctx, current.ID, state.LastEventSeq)
	if err != nil {
		return snapshot.Snapshot{}, err
	}
	for _, stored := range events {
		state, err = projection.Apply(state, stored)
		if err != nil {
			return snapshot.Snapshot{}, fmt.Errorf("safety snapshot replay event seq=%d type=%s: %w", stored.Seq, stored.Type, err)
		}
	}
	if state.LastEventSeq != current.HeadEventSeq {
		return snapshot.Snapshot{}, fmt.Errorf("snapshot head mismatch: timeline=%d replay=%d", current.HeadEventSeq, state.LastEventSeq)
	}
	narrativeState, err := repos.Narrative().Export(ctx, current.ID)
	if err != nil {
		return snapshot.Snapshot{}, err
	}
	state.Narrative = narrativeState
	raw, err := s.newID()
	if err != nil {
		return snapshot.Snapshot{}, err
	}
	created, err := snapshot.New(snapshot.ID(raw), state, s.now())
	if err != nil {
		return snapshot.Snapshot{}, err
	}
	if err := repos.Snapshots().Create(ctx, created); err != nil {
		return snapshot.Snapshot{}, err
	}
	if err := repos.Timelines().SetHeadSnapshot(ctx, current.ID, created.ID); err != nil {
		return snapshot.Snapshot{}, err
	}
	return created, nil
}

func currentCursor(state narrative.State) (*id.ID, *id.ID, *id.ID) {
	var chapter *narrative.Chapter
	for _, candidate := range state.Chapters {
		if candidate.Status != "active" && candidate.Status != "completing" {
			continue
		}
		if chapter == nil || candidate.Number > chapter.Number {
			copy := candidate
			chapter = &copy
		}
	}
	if chapter == nil {
		return nil, nil, nil
	}
	chapterID := chapter.ID
	var scene *narrative.Scene
	for _, candidate := range state.Scenes {
		if candidate.ChapterID != chapter.ID {
			continue
		}
		if candidate.Status != "active" && candidate.Status != "awaiting_player" && candidate.Status != "completing" {
			continue
		}
		if scene == nil || candidate.Number > scene.Number {
			copy := candidate
			scene = &copy
		}
	}
	if scene == nil {
		return &chapterID, nil, nil
	}
	sceneID := scene.ID
	var beat *narrative.Beat
	for _, candidate := range state.Beats {
		if candidate.SceneID != scene.ID || !candidate.IsActive {
			continue
		}
		if beat == nil || candidate.Position > beat.Position {
			copy := candidate
			beat = &copy
		}
	}
	if beat == nil {
		return &chapterID, &sceneID, nil
	}
	beatID := beat.ID
	return &chapterID, &sceneID, &beatID
}
