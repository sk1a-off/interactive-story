package jobs

import (
	"context"
	"errors"
	"github.com/local/interactive-ai-story/backend/internal/domain/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/jobqueue"
	"sync"
	"testing"
	"time"
)

type memQueue struct {
	mu                sync.Mutex
	claimed           jobqueue.ClaimedJob
	completed, failed bool
	cancel            bool
	heartbeats        int
}

func (q *memQueue) Enqueue(context.Context, generation.Job, int) error { return nil }
func (q *memQueue) FindByRequest(context.Context, id.ID, string) (jobqueue.ExistingJob, bool, error) {
	return jobqueue.ExistingJob{}, false, nil
}
func (q *memQueue) FindActiveByTimelineHead(context.Context, id.ID, int64) (jobqueue.ExistingJob, bool, error) {
	return jobqueue.ExistingJob{}, false, nil
}
func (q *memQueue) ClaimNext(context.Context, string, time.Time, time.Duration) (jobqueue.ClaimedJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.claimed, nil
}
func (q *memQueue) Heartbeat(context.Context, id.ID, string, time.Time, time.Duration) error {
	q.mu.Lock()
	q.heartbeats++
	q.mu.Unlock()
	return nil
}
func (q *memQueue) Complete(context.Context, id.ID, string, time.Time) error {
	q.mu.Lock()
	q.completed = true
	q.mu.Unlock()
	return nil
}
func (q *memQueue) FailOrRetry(context.Context, id.ID, string, time.Time, time.Duration, string) error {
	q.mu.Lock()
	q.failed = true
	q.mu.Unlock()
	return nil
}
func (q *memQueue) RequestCancel(context.Context, id.ID, time.Time) error {
	q.mu.Lock()
	q.cancel = true
	q.mu.Unlock()
	return nil
}
func (q *memQueue) IsCancelRequested(context.Context, id.ID) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.cancel, nil
}
func (q *memQueue) RequeueExpired(context.Context, time.Time) (int64, error) { return 0, nil }

type execFn func(context.Context, jobqueue.ClaimedJob) error

func (f execFn) Execute(ctx context.Context, j jobqueue.ClaimedJob) error { return f(ctx, j) }
func jid() id.ID                                                          { return id.MustParse("00000000-0000-4000-8000-000000000901") }
func TestWorkerCompletesSuccessfulJob(t *testing.T) {
	q := &memQueue{claimed: jobqueue.ClaimedJob{Job: generation.Job{ID: jid()}, Reliability: generation.Reliability{AttemptCount: 1, MaxAttempts: 3}}}
	w := Worker{Queue: q, Executor: execFn(func(context.Context, jobqueue.ClaimedJob) error { return nil }), Owner: "w", LeaseTTL: time.Second, HeartbeatEvery: time.Hour, Now: func() time.Time { return time.Unix(1, 0) }}
	if e := w.RunOnce(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !q.completed || q.failed {
		t.Fatal("successful job lifecycle wrong")
	}
}
func TestWorkerRetriesFailure(t *testing.T) {
	q := &memQueue{claimed: jobqueue.ClaimedJob{Job: generation.Job{ID: jid()}, Reliability: generation.Reliability{AttemptCount: 1, MaxAttempts: 3}}}
	w := Worker{Queue: q, Executor: execFn(func(context.Context, jobqueue.ClaimedJob) error { return errors.New("provider down") }), Owner: "w", LeaseTTL: time.Second, HeartbeatEvery: time.Hour, Now: func() time.Time { return time.Unix(1, 0) }}
	if e := w.RunOnce(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !q.failed || q.completed {
		t.Fatal("failed job was not routed to retry policy")
	}
}
func TestWorkerObservesCancellation(t *testing.T) {
	q := &memQueue{claimed: jobqueue.ClaimedJob{Job: generation.Job{ID: jid()}, Reliability: generation.Reliability{AttemptCount: 1, MaxAttempts: 3}}, cancel: true}
	started := make(chan struct{})
	w := Worker{Queue: q, Executor: execFn(func(ctx context.Context, _ jobqueue.ClaimedJob) error { close(started); <-ctx.Done(); return ctx.Err() }), Owner: "w", LeaseTTL: time.Second, HeartbeatEvery: time.Millisecond, Now: time.Now}
	done := make(chan error, 1)
	go func() { done <- w.RunOnce(context.Background()) }()
	<-started
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("worker ignored cancellation")
	}
	if !q.completed {
		t.Fatal("cancelled execution not finalized")
	}
}

func TestWorkerTimeoutRoutesToRetry(t *testing.T) {
	q := &memQueue{claimed: jobqueue.ClaimedJob{Job: generation.Job{ID: jid()}, Reliability: generation.Reliability{AttemptCount: 1, MaxAttempts: 3}}}
	w := Worker{Queue: q, Executor: execFn(func(ctx context.Context, _ jobqueue.ClaimedJob) error { <-ctx.Done(); return ctx.Err() }), Owner: "w", LeaseTTL: time.Second, HeartbeatEvery: time.Hour, ExecutionTimeout: time.Millisecond, Now: time.Now}
	if e := w.RunOnce(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !q.failed || q.completed {
		t.Fatal("timeout must retry/fail, not complete")
	}
}
