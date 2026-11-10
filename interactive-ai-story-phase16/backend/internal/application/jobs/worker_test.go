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
	claimErr          error
	heartbeatErr      error
	completed, failed bool
	cancel            bool
	heartbeats        int
	requeues          int
	requeueNotify     chan struct{}
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
	return q.claimed, q.claimErr
}
func (q *memQueue) Heartbeat(context.Context, id.ID, string, time.Time, time.Duration) error {
	q.mu.Lock()
	q.heartbeats++
	err := q.heartbeatErr
	q.mu.Unlock()
	return err
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
func (q *memQueue) RequeueExpired(context.Context, time.Time) (int64, error) {
	q.mu.Lock()
	q.requeues++
	notify := q.requeueNotify
	q.mu.Unlock()
	if notify != nil {
		select {
		case notify <- struct{}{}:
		default:
		}
	}
	return 0, nil
}

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

func TestWorkerCancelsExecutionWhenHeartbeatLosesLease(t *testing.T) {
	for name, hbErr := range map[string]error{
		"lease lost": generation.ErrLeaseLost,
		"db failure": errors.New("connection reset"),
	} {
		t.Run(name, func(t *testing.T) {
			q := &memQueue{
				claimed:      jobqueue.ClaimedJob{Job: generation.Job{ID: jid()}, Reliability: generation.Reliability{AttemptCount: 1, MaxAttempts: 3}},
				heartbeatErr: hbErr,
			}
			w := Worker{
				Queue: q, Executor: execFn(func(ctx context.Context, _ jobqueue.ClaimedJob) error {
					<-ctx.Done()
					return ctx.Err()
				}), Owner: "w", LeaseTTL: time.Second, HeartbeatEvery: time.Millisecond, Now: time.Now,
			}
			err := w.RunOnce(context.Background())
			if !errors.Is(err, generation.ErrLeaseLost) || errors.Is(err, context.Canceled) {
				t.Fatalf("RunOnce() error = %v, want ErrLeaseLost (not context.Canceled)", err)
			}
			q.mu.Lock()
			defer q.mu.Unlock()
			if q.heartbeats == 0 || q.completed || q.failed {
				t.Fatalf("heartbeats=%d completed=%v failed=%v", q.heartbeats, q.completed, q.failed)
			}
		})
	}
}

// scriptedQueue hands out jobs in order and fails heartbeats for one job only.
type scriptedQueue struct {
	*memQueue
	jobs        []jobqueue.ClaimedJob
	failHB      id.ID
	completedID chan id.ID
}

func (q *scriptedQueue) ClaimNext(context.Context, string, time.Time, time.Duration) (jobqueue.ClaimedJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.jobs) == 0 {
		return jobqueue.ClaimedJob{}, jobqueue.ErrNoJob
	}
	next := q.jobs[0]
	q.jobs = q.jobs[1:]
	return next, nil
}
func (q *scriptedQueue) Heartbeat(_ context.Context, job id.ID, _ string, _ time.Time, _ time.Duration) error {
	if job == q.failHB {
		return generation.ErrLeaseLost
	}
	return nil
}
func (q *scriptedQueue) Complete(_ context.Context, job id.ID, _ string, _ time.Time) error {
	q.completedID <- job
	return nil
}

func TestWorkerRunContinuesAfterHeartbeatFailure(t *testing.T) {
	first := id.MustParse("00000000-0000-4000-8000-000000000911")
	second := id.MustParse("00000000-0000-4000-8000-000000000912")
	rel := generation.Reliability{AttemptCount: 1, MaxAttempts: 3}
	q := &scriptedQueue{
		memQueue:    &memQueue{},
		jobs:        []jobqueue.ClaimedJob{{Job: generation.Job{ID: first}, Reliability: rel}, {Job: generation.Job{ID: second}, Reliability: rel}},
		failHB:      first,
		completedID: make(chan id.ID, 2),
	}
	w := Worker{
		Queue: q, Executor: execFn(func(ctx context.Context, j jobqueue.ClaimedJob) error {
			if j.Job.ID == first {
				<-ctx.Done() // cancelled by the failing heartbeat
				return ctx.Err()
			}
			return nil
		}), Owner: "w", LeaseTTL: time.Minute, HeartbeatEvery: time.Millisecond, RecoveryEvery: time.Hour, Now: time.Now,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, time.Millisecond) }()

	select {
	case got := <-q.completedID:
		if got != second {
			t.Fatalf("completed %s, want second job %s (first lost its lease)", got, second)
		}
	case err := <-done:
		t.Fatalf("Run() exited after heartbeat failure: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("next job was not claimed after heartbeat failure")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled on shutdown", err)
	}
}

func TestWorkerRecoversExpiredLeasesPeriodically(t *testing.T) {
	notify := make(chan struct{}, 2)
	q := &memQueue{claimErr: jobqueue.ErrNoJob, requeueNotify: notify}
	w := Worker{Queue: q, Owner: "w", LeaseTTL: time.Minute, RecoveryEvery: 5 * time.Millisecond, Now: time.Now}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, time.Hour) }()

	for calls := 0; calls < 2; calls++ {
		select {
		case <-notify:
		case <-time.After(time.Second):
			t.Fatal("worker did not run periodic lease recovery")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.requeues < 2 {
		t.Fatalf("requeues=%d, want startup and periodic recovery", q.requeues)
	}
}
