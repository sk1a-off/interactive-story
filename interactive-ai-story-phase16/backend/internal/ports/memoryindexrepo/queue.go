package memoryindexrepo

import (
	"context"
	"errors"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

var (
	ErrNoJob     = errors.New("no memory indexing job available")
	ErrLeaseLost = errors.New("memory indexing lease lost")
)

type Job struct {
	ID             id.ID
	StoryID        id.ID
	TimelineID     timeline.ID
	SourceKind     string
	SourceID       id.ID
	SourceEventSeq int64
	Attempts       int
	MaxAttempts    int
	LeaseOwner     string
	LeaseToken     id.ID
	LeaseUntil     time.Time
}

type Queue interface {
	Claim(context.Context, string, time.Time, time.Duration) (Job, error)
	Heartbeat(context.Context, Job, time.Time, time.Duration) error
	Complete(context.Context, Job, time.Time) error
	Retry(context.Context, Job, time.Time, time.Duration, string) error
}
