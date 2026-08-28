package timeline

import (
	"errors"
	"fmt"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
)

type ID id.ID
type SnapshotID id.ID
type SavePointID id.ID

type Status string

const (
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
	StatusDeleted  Status = "deleted"
)

var (
	ErrInvalidTimeline = errors.New("invalid timeline")
	ErrTimelineClosed  = errors.New("timeline cannot accept mutation")
)

type HeadConflictError struct {
	Expected int64
	Actual   int64
}

func (e HeadConflictError) Error() string {
	return fmt.Sprintf("timeline head moved: expected %d, actual %d", e.Expected, e.Actual)
}

type Timeline struct {
	ID               ID
	StoryID          story.ID
	ParentTimelineID *ID
	ForkedFromSaveID *SavePointID
	Name             string
	Status           Status
	HeadEventSeq     int64
	HeadSnapshotID   *SnapshotID
}

func New(timelineID ID, storyID story.ID, name string) (Timeline, error) {
	name = strings.TrimSpace(name)
	if id.ID(timelineID).IsZero() || id.ID(storyID).IsZero() || name == "" {
		return Timeline{}, ErrInvalidTimeline
	}
	return Timeline{ID: timelineID, StoryID: storyID, Name: name, Status: StatusActive}, nil
}

func (t Timeline) CanAcceptMutation() error {
	if t.Status != StatusActive {
		return ErrTimelineClosed
	}
	return nil
}

func (t Timeline) CheckExpectedHead(expected int64) error {
	if expected != t.HeadEventSeq {
		return HeadConflictError{Expected: expected, Actual: t.HeadEventSeq}
	}
	return nil
}
