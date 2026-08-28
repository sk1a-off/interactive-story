package jobs

import (
	"context"
	"errors"
	"github.com/local/interactive-ai-story/backend/internal/domain/generation"
	"github.com/local/interactive-ai-story/backend/internal/ports/jobqueue"
	"time"
)

type Executor interface {
	Execute(context.Context, jobqueue.ClaimedJob) error
}

type Worker struct {
	Queue            jobqueue.Queue
	Executor         Executor
	Owner            string
	LeaseTTL         time.Duration
	HeartbeatEvery   time.Duration
	ExecutionTimeout time.Duration
	Backoff          func(int) time.Duration
	Now              func() time.Time
}

func (w Worker) RunOnce(ctx context.Context) error {
	now := w.Now()
	claimed, err := w.Queue.ClaimNext(ctx, w.Owner, now, w.LeaseTTL)
	if err != nil {
		return err
	}
	var runCtx context.Context
	var cancel context.CancelFunc
	if w.ExecutionTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, w.ExecutionTimeout)
	} else {
		runCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		ticker := time.NewTicker(w.HeartbeatEvery)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if ok, e := w.Queue.IsCancelRequested(runCtx, claimed.Job.ID); e == nil && ok {
					cancel()
					return
				}
				_ = w.Queue.Heartbeat(runCtx, claimed.Job.ID, w.Owner, w.Now(), w.LeaseTTL)
			}
		}
	}()
	execErr := w.Executor.Execute(runCtx, claimed)
	cancel()
	<-hbDone
	finalCtx, finalCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finalCancel()
	if execErr == nil {
		return w.Queue.Complete(finalCtx, claimed.Job.ID, w.Owner, w.Now())
	}
	if errors.Is(execErr, context.Canceled) {
		if requested, e := w.Queue.IsCancelRequested(finalCtx, claimed.Job.ID); e == nil && requested {
			return w.Queue.Complete(finalCtx, claimed.Job.ID, w.Owner, w.Now())
		}
		// Worker shutdown or lease loss is not successful completion. Let lease
		// recovery requeue it if finalization cannot safely acquire the lease.
		return execErr
	}
	backoff := time.Second
	if w.Backoff != nil {
		backoff = w.Backoff(claimed.Reliability.AttemptCount)
	}
	return w.Queue.FailOrRetry(finalCtx, claimed.Job.ID, w.Owner, w.Now(), backoff, execErr.Error())
}

func DefaultBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := time.Second << min(attempt-1, 5)
	return d
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = generation.ErrLeaseLost

func (w Worker) Run(ctx context.Context, poll time.Duration) error {
	if poll <= 0 {
		poll = 250 * time.Millisecond
	}
	if _, err := w.Queue.RequeueExpired(ctx, w.Now()); err != nil {
		return err
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		err := w.RunOnce(ctx)
		if err != nil && !errors.Is(err, jobqueue.ErrNoJob) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
