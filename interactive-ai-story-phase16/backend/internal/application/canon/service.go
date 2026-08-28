package canon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/projection"
	"github.com/local/interactive-ai-story/backend/internal/domain/snapshot"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/repositories"
	"github.com/local/interactive-ai-story/backend/internal/ports/tx"
)

var ErrHeadMoved = errors.New("timeline head moved")

type Observer interface {
	AfterCommit(context.Context, timeline.ID, []event.StoredEvent)
}

type Service struct {
	uow      tx.UnitOfWork
	now      func() time.Time
	Observer Observer
}

func NewService(uow tx.UnitOfWork, now func() time.Time) Service {
	return Service{uow: uow, now: now}
}

func (s Service) AppendSemantic(ctx context.Context, timelineID timeline.ID, expectedHead int64, pending []event.PendingEvent) ([]event.StoredEvent, error) {
	if len(pending) == 0 {
		return nil, nil
	}
	for _, e := range pending {
		if err := e.Validate(); err != nil {
			return nil, err
		}
	}

	var committed []event.StoredEvent
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		current, err := repos.Timelines().Lock(ctx, timelineID)
		if err != nil {
			return err
		}
		if err := current.CanAcceptMutation(); err != nil {
			return err
		}
		if err := current.CheckExpectedHead(expectedHead); err != nil {
			return fmt.Errorf("%w: %v", ErrHeadMoved, err)
		}

		committed = make([]event.StoredEvent, 0, len(pending))
		seq := current.HeadEventSeq
		for _, p := range pending {
			eventID, err := id.New()
			if err != nil {
				return err
			}
			seq++
			stored := event.StoredEvent{ID: event.ID(eventID), TimelineID: timelineID, Seq: seq, Type: p.Type, SchemaVersion: p.SchemaVersion, Payload: append([]byte(nil), p.Payload...), GenerationID: p.GenerationID, CreatedAt: s.now()}
			if err := stored.Validate(); err != nil {
				return err
			}
			committed = append(committed, stored)
		}
		baseNarrative, err := repos.Narrative().Export(ctx, timelineID)
		if err != nil {
			return err
		}
		state := projection.TimelineState{SchemaVersion: projection.SchemaVersion, TimelineID: timelineID, LastEventSeq: current.HeadEventSeq, AppliedEventCount: current.HeadEventSeq, Narrative: baseNarrative}
		for _, stored := range committed {
			state, err = projection.Apply(state, stored)
			if err != nil {
				return err
			}
		}
		if err := repos.Narrative().Materialize(ctx, state.Narrative); err != nil {
			return err
		}
		if err := repos.Events().Append(ctx, timelineID, committed); err != nil {
			return err
		}
		return repos.Timelines().AdvanceHead(ctx, timelineID, seq)
	})
	if err != nil {
		return nil, err
	}
	if s.Observer != nil {
		s.Observer.AfterCommit(ctx, timelineID, committed)
	}
	return committed, nil
}

func (s Service) Rebuild(ctx context.Context, timelineID timeline.ID) (projection.TimelineState, error) {
	var state projection.TimelineState
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		current, err := repos.Timelines().Get(ctx, timelineID)
		if err != nil {
			return err
		}
		state = projection.Empty(timelineID)
		if current.HeadSnapshotID != nil {
			base, err := repos.Snapshots().Get(ctx, snapshot.ID(*current.HeadSnapshotID))
			if err != nil {
				return err
			}
			state = base.State
		}
		events, err := repos.Events().List(ctx, timelineID, state.LastEventSeq)
		if err != nil {
			return err
		}
		for _, stored := range events {
			state, err = projection.Apply(state, stored)
			if err != nil {
				return fmt.Errorf("replay event seq=%d type=%s: %w", stored.Seq, stored.Type, err)
			}
		}
		if state.LastEventSeq != current.HeadEventSeq {
			return fmt.Errorf("projection head mismatch: timeline=%d replay=%d", current.HeadEventSeq, state.LastEventSeq)
		}
		return nil
	})
	return state, err
}

func (s Service) CreateSnapshot(ctx context.Context, timelineID timeline.ID) (snapshot.Snapshot, error) {
	var created snapshot.Snapshot
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		current, err := repos.Timelines().Lock(ctx, timelineID)
		if err != nil {
			return err
		}
		state := projection.Empty(timelineID)
		if current.HeadSnapshotID != nil {
			base, err := repos.Snapshots().Get(ctx, snapshot.ID(*current.HeadSnapshotID))
			if err != nil {
				return err
			}
			state = base.State
		}
		events, err := repos.Events().List(ctx, timelineID, state.LastEventSeq)
		if err != nil {
			return err
		}
		for _, stored := range events {
			state, err = projection.Apply(state, stored)
			if err != nil {
				return fmt.Errorf("snapshot replay event seq=%d type=%s: %w", stored.Seq, stored.Type, err)
			}
		}
		if state.LastEventSeq != current.HeadEventSeq {
			return fmt.Errorf("projection head mismatch: timeline=%d replay=%d", current.HeadEventSeq, state.LastEventSeq)
		}
		narrativeState, err := repos.Narrative().Export(ctx, timelineID)
		if err != nil {
			return err
		}
		state.Narrative = narrativeState
		sid, err := id.New()
		if err != nil {
			return err
		}
		created, err = snapshot.New(snapshot.ID(sid), state, s.now())
		if err != nil {
			return err
		}
		if err := repos.Snapshots().Create(ctx, created); err != nil {
			return err
		}
		return repos.Timelines().SetHeadSnapshot(ctx, timelineID, created.ID)
	})
	return created, err
}
