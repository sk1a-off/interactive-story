package generation

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	domaincfg "github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	domainjob "github.com/local/interactive-ai-story/backend/internal/domain/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domainprompt "github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/aiconfigrepo"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationmeta"
	"github.com/local/interactive-ai-story/backend/internal/ports/jobqueue"
	"github.com/local/interactive-ai-story/backend/internal/ports/promptsetrepo"
)

type DurableRunner struct {
	Queue       jobqueue.Queue
	Config      aiconfigrepo.Repository
	Metadata    generationmeta.Source
	PromptSets  promptsetrepo.Repository
	Hub         *Hub
	Updates     UpdateStore
	MaxAttempts int
}

func (r *DurableRunner) Submit(ctx context.Context, key string, action PlayerAction) (id.ID, error) {
	if r.Queue == nil || r.Config == nil || r.Metadata == nil {
		return "", errors.New("durable generation unavailable")
	}
	if key != "" {
		if existing, ok, err := r.Queue.FindByRequest(ctx, id.ID(action.TimelineID), key); err != nil {
			return "", err
		} else if ok {
			return existing, nil
		}
	}
	if existing, ok, err := r.Queue.FindActiveByTimelineHead(ctx, id.ID(action.TimelineID), action.ExpectedHead); err != nil {
		return "", err
	} else if ok {
		return existing, nil
	}
	storyID, err := r.Metadata.StoryForTimeline(ctx, action.TimelineID)
	if err != nil {
		return "", err
	}
	cfg, err := r.Config.Active(ctx)
	if err != nil {
		return "", err
	}
	gid, err := id.New()
	if err != nil {
		return "", err
	}
	action.StoryID = id.ID(storyID)
	var promptSetID id.ID
	if r.PromptSets != nil {
		promptSet, promptErr := r.PromptSets.Active(ctx)
		if promptErr != nil {
			return "", promptErr
		}
		promptSetID = promptSet.ID
	}
	job, err := domainjob.NewJobWithPrompt(gid, id.ID(storyID), id.ID(action.TimelineID), action.ExpectedHead, cfg, promptSetID, aiport.KindStoryLLM, key)
	if err != nil {
		return "", err
	}
	job.RequestPayload, err = json.Marshal(action)
	if err != nil {
		return "", err
	}
	attempts := r.MaxAttempts
	if attempts < 1 {
		attempts = 3
	}
	if err = r.Queue.Enqueue(ctx, job, attempts); err != nil {
		if key != "" {
			if existing, ok, findErr := r.Queue.FindByRequest(ctx, id.ID(action.TimelineID), key); findErr == nil && ok {
				return existing, nil
			}
		}
		// The partial unique index closes the race between the lookup above and
		// concurrent inserts. Return the already active job so every caller
		// observes one generation id for one timeline head.
		if existing, ok, findErr := r.Queue.FindActiveByTimelineHead(ctx, id.ID(action.TimelineID), action.ExpectedHead); findErr == nil && ok {
			return existing, nil
		}
		return "", err
	}
	return gid, nil
}

func (r *DurableRunner) Subscribe(g id.ID) (<-chan Update, func()) {
	if r.Hub == nil {
		r.Hub = NewHub()
	}
	return r.Hub.Subscribe(g, 32)
}

func (r *DurableRunner) Replay(ctx context.Context, g id.ID, after int64) ([]Update, error) {
	if r.Updates == nil {
		return nil, nil
	}
	return r.Updates.List(ctx, g, after)
}
func (r *DurableRunner) Cancel(ctx context.Context, g id.ID) error {
	return r.Queue.RequestCancel(ctx, g, time.Now().UTC())
}

type PipelineResolver func(context.Context, domaincfg.Revision, *domainprompt.Revision) (Pipeline, error)

type DurableExecutor struct {
	Config  aiconfigrepo.Repository
	Prompts promptsetrepo.Repository
	Resolve PipelineResolver
	Hub     *Hub
	Sink    Sink
}

type attemptAwareSink struct {
	Sink
	retryRemaining bool
}

func (s attemptAwareSink) Publish(ctx context.Context, update Update) {
	if update.Phase == PhaseFailed && s.retryRemaining {
		update.Phase = PhaseRetrying
	}
	if s.Sink != nil {
		s.Sink.Publish(ctx, update)
	}
}

func (e DurableExecutor) Execute(ctx context.Context, claimed jobqueue.ClaimedJob) error {
	if e.Config == nil || e.Resolve == nil {
		return errors.New("generation executor unavailable")
	}
	var action PlayerAction
	if err := json.Unmarshal(claimed.Job.RequestPayload, &action); err != nil {
		return err
	}
	cfg, err := e.Config.Get(ctx, claimed.Job.ConfigRevisionID)
	if err != nil {
		return err
	}
	var promptRev *domainprompt.Revision
	if !claimed.Job.PromptSetRevisionID.IsZero() && e.Prompts != nil {
		resolved, promptErr := e.Prompts.Get(ctx, claimed.Job.PromptSetRevisionID)
		if promptErr != nil {
			return promptErr
		}
		promptRev = &resolved
	}
	pipeline, err := e.Resolve(ctx, cfg, promptRev)
	if err != nil {
		return err
	}
	if e.Sink != nil {
		pipeline.Sink = attemptAwareSink{Sink: e.Sink, retryRemaining: claimed.Reliability.AttemptCount < claimed.Reliability.MaxAttempts}
	} else {
		pipeline.Sink = attemptAwareSink{Sink: e.Hub, retryRemaining: claimed.Reliability.AttemptCount < claimed.Reliability.MaxAttempts}
	}
	_, err = pipeline.Run(ctx, claimed.Job.ID, action)
	return err
}
