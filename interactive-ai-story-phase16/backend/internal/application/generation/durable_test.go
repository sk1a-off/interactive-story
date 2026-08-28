package generation

import (
	"context"
	domaincfg "github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	domainjob "github.com/local/interactive-ai-story/backend/internal/domain/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/jobqueue"
	"testing"
	"time"
)

type dq struct{ jobs map[string]domainjob.Job }

type capturedSink struct{ updates []Update }

func (s *capturedSink) Publish(_ context.Context, update Update) {
	s.updates = append(s.updates, update)
}

func (q *dq) Enqueue(_ context.Context, j domainjob.Job, _ int) error {
	if q.jobs == nil {
		q.jobs = map[string]domainjob.Job{}
	}
	q.jobs[j.RequestID] = j
	return nil
}
func (q *dq) FindByRequest(_ context.Context, _ id.ID, k string) (id.ID, bool, error) {
	j, ok := q.jobs[k]
	return j.ID, ok, nil
}
func (q *dq) FindActiveByTimelineHead(_ context.Context, timelineID id.ID, expectedHead int64) (id.ID, bool, error) {
	for _, j := range q.jobs {
		if j.TimelineID == timelineID && j.ExpectedHeadEventSeq == expectedHead {
			return j.ID, true, nil
		}
	}
	return "", false, nil
}
func (q *dq) ClaimNext(context.Context, string, time.Time, time.Duration) (jobqueue.ClaimedJob, error) {
	return jobqueue.ClaimedJob{}, jobqueue.ErrNoJob
}
func (q *dq) Heartbeat(context.Context, id.ID, string, time.Time, time.Duration) error { return nil }
func (q *dq) Complete(context.Context, id.ID, string, time.Time) error                 { return nil }
func (q *dq) FailOrRetry(context.Context, id.ID, string, time.Time, time.Duration, string) error {
	return nil
}
func (q *dq) RequestCancel(context.Context, id.ID, time.Time) error    { return nil }
func (q *dq) IsCancelRequested(context.Context, id.ID) (bool, error)   { return false, nil }
func (q *dq) RequeueExpired(context.Context, time.Time) (int64, error) { return 0, nil }

type cfgRepo struct{ v domaincfg.Revision }

func (c cfgRepo) CreateRevision(context.Context, domaincfg.CreateRevisionCommand) (domaincfg.Revision, error) {
	return domaincfg.Revision{}, nil
}
func (c cfgRepo) Get(context.Context, id.ID) (domaincfg.Revision, error) { return c.v, nil }
func (c cfgRepo) List(context.Context) ([]domaincfg.Revision, error) {
	return []domaincfg.Revision{c.v}, nil
}
func (c cfgRepo) Active(context.Context) (domaincfg.Revision, error) { return c.v, nil }
func (c cfgRepo) Activate(context.Context, id.ID) error              { return nil }

type meta struct{ sid story.ID }

func (m meta) StoryForTimeline(context.Context, timeline.ID) (story.ID, error) { return m.sid, nil }
func TestDurableRunnerPersistsActionAndIdempotency(t *testing.T) {
	cfg, _ := domaincfg.NewRevision(id.MustParse("00000000-0000-4000-8000-000000000701"), 1,
		ai.ProviderIdentity{Kind: ai.KindStoryLLM, Provider: "fake", Model: "story"},
		ai.ProviderIdentity{Kind: ai.KindEmbedding, Provider: "fake", Model: "embed"},
		ai.ProviderIdentity{Kind: ai.KindImage, Provider: "fake", Model: "image"})
	q := &dq{}
	tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000702"))
	sid := story.ID(id.MustParse("00000000-0000-4000-8000-000000000703"))
	r := DurableRunner{Queue: q, Config: cfgRepo{cfg}, Metadata: meta{sid: sid}, Hub: NewHub()}
	first, e := r.Submit(context.Background(), "request-1", PlayerAction{TimelineID: tid, ExpectedHead: 4, Text: "look"})
	if e != nil {
		t.Fatal(e)
	}
	second, e := r.Submit(context.Background(), "request-1", PlayerAction{TimelineID: tid, ExpectedHead: 4, Text: "look"})
	if e != nil {
		t.Fatal(e)
	}
	if first != second {
		t.Fatal("durable idempotency returned a different generation")
	}
	third, e := r.Submit(context.Background(), "request-2", PlayerAction{TimelineID: tid, ExpectedHead: 4, Text: "look"})
	if e != nil {
		t.Fatal(e)
	}
	if first != third || len(q.jobs) != 1 {
		t.Fatal("same timeline head created more than one active generation")
	}
	j := q.jobs["request-1"]
	if len(j.RequestPayload) == 0 || j.StoryID != id.ID(sid) || j.ConfigRevisionID != cfg.ID {
		t.Fatal("durable job did not pin request/story/config")
	}
}

func TestAttemptAwareSinkOnlyPublishesFailedForFinalAttempt(t *testing.T) {
	captured := &capturedSink{}
	attemptAwareSink{Sink: captured, retryRemaining: true}.Publish(context.Background(), Update{Phase: PhaseFailed, Error: "invalid json"})
	attemptAwareSink{Sink: captured, retryRemaining: false}.Publish(context.Background(), Update{Phase: PhaseFailed, Error: "invalid json"})
	if len(captured.updates) != 2 || captured.updates[0].Phase != PhaseRetrying || captured.updates[1].Phase != PhaseFailed {
		t.Fatalf("unexpected attempt phases: %#v", captured.updates)
	}
}
