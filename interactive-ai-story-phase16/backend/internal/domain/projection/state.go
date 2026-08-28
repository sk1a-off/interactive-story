package projection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

const SchemaVersion = 3

var ErrNonContiguousEvent = errors.New("event sequence is not contiguous")

type TimelineState struct {
	SchemaVersion     int             `json:"schema_version"`
	TimelineID        timeline.ID     `json:"timeline_id"`
	LastEventSeq      int64           `json:"last_event_seq"`
	AppliedEventCount int64           `json:"applied_event_count"`
	Narrative         narrative.State `json:"narrative"`
}

func Empty(timelineID timeline.ID) TimelineState {
	return TimelineState{SchemaVersion: SchemaVersion, TimelineID: timelineID, Narrative: narrative.NewState(id.ID(timelineID))}
}

func Apply(state TimelineState, e event.StoredEvent) (TimelineState, error) {
	if e.TimelineID != state.TimelineID || e.Seq != state.LastEventSeq+1 {
		return TimelineState{}, ErrNonContiguousEvent
	}
	state.SchemaVersion = SchemaVersion
	state.Narrative.EnsureWorldMaps()
	if err := applySemantic(&state, e); err != nil {
		return TimelineState{}, err
	}
	state.LastEventSeq = e.Seq
	state.AppliedEventCount++
	return state, nil
}

func Replay(timelineID timeline.ID, events []event.StoredEvent) (TimelineState, error) {
	state := Empty(timelineID)
	for _, e := range events {
		var err error
		state, err = Apply(state, e)
		if err != nil {
			return TimelineState{}, err
		}
	}
	return state, nil
}

func CanonicalJSON(state TimelineState) ([]byte, error) {
	return json.Marshal(state)
}

func Hash(state TimelineState) (string, error) {
	raw, err := CanonicalJSON(state)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func CloneForTimeline(state TimelineState, target timeline.ID) TimelineState {
	state.TimelineID = target
	state.Narrative = state.Narrative.CloneForTimeline(id.ID(target))
	return state
}

func ForkForTimeline(state TimelineState, target timeline.ID, newID narrative.IDGenerator) (TimelineState, error) {
	state.TimelineID = target
	forked, err := state.Narrative.ForkForTimeline(id.ID(target), newID)
	if err != nil {
		return TimelineState{}, err
	}
	state.Narrative = forked
	return state, nil
}
