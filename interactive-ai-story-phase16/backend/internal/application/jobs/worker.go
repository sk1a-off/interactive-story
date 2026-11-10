package jobs

import (
	"context"
	"errors"
	"fmt"
	"github.com/local/interactive-ai-story/backend/internal/domain/generation"
	"github.com/local/interactive-ai-story/backend/internal/ports/jobqueue"
	"log/slog"
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
	RecoveryEvery    time.Duration
	ExecutionTimeout time.Duration
	Backoff          func(int) time.Duration
	Now              func() time.Time
	// Logger is optional; when set, non-fatal lease losses are logged.
	Logger *slog.Logger
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
	heartbeatEvery := w.HeartbeatEvery
	if heartbeatEvery <= 0 {
		heartbeatEvery = 10 * time.Second
	}
	// heartbeatErr records why the heartbeat goroutine cancelled the run. It is
	// written only by that goroutine and read only after hbDone is closed.
	var heartbeatErr error
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		ticker := time.NewTicker(heartbeatEvery)
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
				if e := w.Queue.Heartbeat(runCtx, claimed.Job.ID, w.Owner, w.Now(), w.LeaseTTL); e != nil {
					// A failure caused by our own context ending (shutdown or
					// execution timeout) is not a lease problem.
					if runCtx.Err() == nil {
						heartbeatErr = e
					}
					cancel()
					return
				}
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
	if heartbeatErr != nil && ctx.Err() == nil {
		// The lease could not be renewed (lost lease or DB failure). Do not
		// finalize: the lease may already belong to another worker, and lease
		// recovery requeues the job otherwise. Report it as ErrLeaseLost so the
		// run loop keeps going instead of treating it as shutdown.
		if errors.Is(heartbeatErr, generation.ErrLeaseLost) {
			return fmt.Errorf("jobs: job %s: heartbeat: %w", claimed.Job.ID, heartbeatErr)
		}
		return fmt.Errorf("jobs: job %s: heartbeat: %w: %v", claimed.Job.ID, generation.ErrLeaseLost, heartbeatErr)
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

func (w Worker) Run(ctx context.Context, poll time.Duration) error {
	if poll <= 0 {
		poll = 250 * time.Millisecond
	}
	if _, err := w.Queue.RequeueExpired(ctx, w.Now()); err != nil {
		return err
	}
	recoveryEvery := w.RecoveryEvery
	if recoveryEvery <= 0 {
		recoveryEvery = w.LeaseTTL / 2
		if recoveryEvery <= 0 || recoveryEvery > 5*time.Second {
			recoveryEvery = 5 * time.Second
		}
	}
	pollTicker := time.NewTicker(poll)
	defer pollTicker.Stop()
	recoveryTicker := time.NewTicker(recoveryEvery)
	defer recoveryTicker.Stop()
	for {
		err := w.RunOnce(ctx)
		switch {
		case err == nil, errors.Is(err, jobqueue.ErrNoJob):
		case errors.Is(err, generation.ErrLeaseLost) && ctx.Err() == nil:
			// Losing one job's lease must not stop the worker: lease recovery
			// requeues that job and we keep claiming new ones.
			if w.Logger != nil {
				w.Logger.Warn("generation job lease lost; worker continues", "owner", w.Owner, "error", err)
			}
		default:
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-recoveryTicker.C:
			if _, err := w.Queue.RequeueExpired(ctx, w.Now()); err != nil {
				return err
			}
		case <-pollTicker.C:
		}
	}
}
