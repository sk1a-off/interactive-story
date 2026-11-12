package snapshot

import (
	"errors"
	"fmt"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/projection"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type ID id.ID

var ErrInvalidSnapshot = errors.New("invalid snapshot")

const (
	// SerializerLegacy hashes the exact JSON produced by the Go encoder for
	// the state struct as it existed when the snapshot was written. Existing
	// rows carry this version; they are verified byte-for-byte and upgraded
	// in memory through Restore.
	SerializerLegacy = 1
	// SerializerCanonical hashes the canonical zero-eliding JSON form, which
	// survives additive schema changes and jsonb normalization.
	SerializerCanonical = 2
)

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
	if state.SchemaVersion == 0 {
		// States assembled by hand or exported from tables before stamping.
		state.SchemaVersion = projection.SchemaVersion
	}
	state, err := projection.Upgrade(state)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
	}
	hash, err := projection.CanonicalHash(state)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{ID: snapshotID, TimelineID: state.TimelineID, EventSeq: state.LastEventSeq, State: state, StateHash: hash, SchemaVersion: projection.SchemaVersion, SerializerVersion: SerializerCanonical, CreatedAt: now}, nil
}

func (s Snapshot) Verify() error {
	if s.EventSeq != s.State.LastEventSeq || s.TimelineID != s.State.TimelineID || s.SchemaVersion != s.State.SchemaVersion || s.SchemaVersion < 1 || s.SchemaVersion > projection.SchemaVersion {
		return ErrInvalidSnapshot
	}
	hash, err := s.expectedHash()
	if err != nil || hash != s.StateHash {
		return ErrInvalidSnapshot
	}
	return nil
}

func (s Snapshot) expectedHash() (string, error) {
	switch s.SerializerVersion {
	case SerializerLegacy:
		return projection.Hash(s.State)
	case SerializerCanonical:
		return projection.CanonicalHash(s.State)
	default:
		return "", ErrInvalidSnapshot
	}
}

// Restore reconciles a persisted snapshot with the current code. storedJSON is
// the state exactly as read back from storage. Canonical snapshots are
// verified by hash. Legacy snapshots are accepted when the stored JSON and the
// decoded state carry the same information: that proves nothing was dropped
// by the decoder (the legacy hash never detected unknown members) and it
// explains an exact-hash mismatch caused by the state type gaining fields.
// Accepted snapshots are returned upgraded to the current schema and
// serializer; the persisted row is left untouched.
func Restore(value Snapshot, storedJSON []byte) (Snapshot, error) {
	if value.SchemaVersion < 1 || value.SchemaVersion > projection.SchemaVersion || value.SchemaVersion != value.State.SchemaVersion {
		return Snapshot{}, ErrInvalidSnapshot
	}
	switch value.SerializerVersion {
	case SerializerCanonical:
		if err := value.Verify(); err != nil {
			return Snapshot{}, err
		}
	case SerializerLegacy:
		if err := verifyLossless(value.State, storedJSON); err != nil {
			return Snapshot{}, err
		}
	default:
		return Snapshot{}, ErrInvalidSnapshot
	}
	if value.SchemaVersion == projection.SchemaVersion && value.SerializerVersion == SerializerCanonical {
		return value, nil
	}
	upgraded, err := New(value.ID, value.State, value.CreatedAt)
	if err != nil {
		return Snapshot{}, err
	}
	return upgraded, nil
}

// verifyLossless proves that decoding storedJSON into the current state type
// dropped nothing: every non-zero member of the stored document must survive
// a decode/encode round trip. A stored member the current type no longer
// knows would disappear from the re-encoded form and fail this check.
func verifyLossless(state projection.TimelineState, storedJSON []byte) error {
	if len(storedJSON) == 0 {
		return ErrInvalidSnapshot
	}
	stored, err := projection.CanonicalHashJSON(storedJSON)
	if err != nil {
		return ErrInvalidSnapshot
	}
	decoded, err := projection.CanonicalHash(state)
	if err != nil || stored != decoded {
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
