package jobqueue

import (
	"context"
	"errors"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

var ErrNoJob = errors.New("no generation job available")

type ExistingJob struct {
	ID         id.ID
	ActionHash string
}

type ClaimedJob struct {
	Job         generation.Job
	Reliability generation.Reliability
}

type Queue interface {
	Enqueue(context.Context, generation.Job, int) error
	FindByRequest(context.Context, id.ID, string) (ExistingJob, bool, error)
	FindActiveByTimelineHead(context.Context, id.ID, int64) (ExistingJob, bool, error)
	ClaimNext(context.Context, string, time.Time, time.Duration) (ClaimedJob, error)
	Heartbeat(context.Context, id.ID, string, time.Time, time.Duration) error
	Complete(context.Context, id.ID, string, time.Time) error
	FailOrRetry(context.Context, id.ID, string, time.Time, time.Duration, string) error
	RequestCancel(context.Context, id.ID, time.Time) error
	IsCancelRequested(context.Context, id.ID) (bool, error)
	RequeueExpired(context.Context, time.Time) (int64, error)
}
