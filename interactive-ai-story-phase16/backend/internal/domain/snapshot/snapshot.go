package snapshot

import (
	"errors"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/projection"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type ID id.ID

var ErrInvalidSnapshot = errors.New("invalid snapshot")

type Snapshot struct {
	ID                ID
	TimelineID        timeline.ID
	EventSeq          int64
	State             projection.TimelineState
	StateHash         string
	SchemaVersion     int
	SerializerVersion int
	CreatedAt         time.Time
}

func New(snapshotID ID, state projection.TimelineState, now time.Time) (Snapshot, error) {
	if id.ID(snapshotID).IsZero() || id.ID(state.TimelineID).IsZero() || state.LastEventSeq < 0 {
		return Snapshot{}, ErrInvalidSnapshot
	}
	hash, err := projection.Hash(state)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{ID: snapshotID, TimelineID: state.TimelineID, EventSeq: state.LastEventSeq, State: state, StateHash: hash, SchemaVersion: projection.SchemaVersion, SerializerVersion: 1, CreatedAt: now}, nil
}

func (s Snapshot) Verify() error {
	if s.EventSeq != s.State.LastEventSeq || s.TimelineID != s.State.TimelineID || s.SchemaVersion != s.State.SchemaVersion || s.SchemaVersion < 1 || s.SchemaVersion > projection.SchemaVersion || s.SerializerVersion < 1 {
		return ErrInvalidSnapshot
	}
	hash, err := projection.Hash(s.State)
	if err != nil || hash != s.StateHash {
		return ErrInvalidSnapshot
	}
	return nil
}

func CloneForTimeline(snapshotID ID, source Snapshot, target timeline.ID, now time.Time) (Snapshot, error) {
	if err := source.Verify(); err != nil {
		return Snapshot{}, err
	}
	state := projection.CloneForTimeline(source.State, target)
	return New(snapshotID, state, now)
}

func CloneForTimelineWithIDs(snapshotID ID, source Snapshot, target timeline.ID, now time.Time, newID projectionIDGenerator) (Snapshot, error) {
	if err := source.Verify(); err != nil {
		return Snapshot{}, err
	}
	state, err := projection.ForkForTimeline(source.State, target, func() (id.ID, error) { return newID() })
	if err != nil {
		return Snapshot{}, err
	}
	return New(snapshotID, state, now)
}

type projectionIDGenerator func() (id.ID, error)
