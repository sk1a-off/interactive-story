package event

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type ID id.ID
type GenerationID id.ID

var ErrInvalidEvent = errors.New("invalid story event")

type StoredEvent struct {
	ID            ID
	TimelineID    timeline.ID
	Seq           int64
	Type          string
	SchemaVersion int
	Payload       json.RawMessage
	GenerationID  *GenerationID
	CreatedAt     time.Time
}

func (e StoredEvent) Validate() error {
	if id.ID(e.ID).IsZero() || id.ID(e.TimelineID).IsZero() || e.Seq < 1 || e.Type == "" || e.SchemaVersion < 1 || !json.Valid(e.Payload) {
		return ErrInvalidEvent
	}
	return nil
}

type PendingEvent struct {
	Type          string
	SchemaVersion int
	Payload       json.RawMessage
	GenerationID  *GenerationID
}

func (e PendingEvent) Validate() error {
	if e.Type == "" || e.SchemaVersion < 1 || !json.Valid(e.Payload) {
		return ErrInvalidEvent
	}
	return nil
}
