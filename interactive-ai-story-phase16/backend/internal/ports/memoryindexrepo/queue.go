package memoryindexrepo

import (
	"context"
	"errors"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/memory"
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
	IndexRevision  int64
}

type Queue interface {
	Claim(context.Context, string, time.Time, time.Duration) (Job, error)
	Heartbeat(context.Context, Job, time.Time, time.Duration) error
	Complete(context.Context, Job, time.Time) error
	Retry(context.Context, Job, time.Time, time.Duration, string) error
}

type Profile struct {
	Provider string
	Model    string
	Revision int64
}

type Store interface {
	Load(context.Context, Job) ([]domain.Document, error)
	Changed(context.Context, Job, Profile, []domain.Document) ([]domain.Document, error)
	// Publish must atomically write versions, advance the publication cursor,
	// and complete the job after checking its current unexpired lease token.
	Publish(context.Context, Job, Profile, []domain.Document, map[string][]float32, time.Time) error
}

// Recoverer is implemented by queues that can requeue jobs which exhausted
// their attempts, after a cooldown and up to a bounded number of recoveries.
type Recoverer interface {
	RecoverFailed(ctx context.Context, now time.Time, maxRecoveries int) (int64, error)
}
