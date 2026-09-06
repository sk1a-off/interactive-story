package generation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	domaincfg "github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	domainjob "github.com/local/interactive-ai-story/backend/internal/domain/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domainprompt "github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/aiconfigrepo"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationattempts"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationmeta"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationmetrics"
	"github.com/local/interactive-ai-story/backend/internal/ports/jobqueue"
	"github.com/local/interactive-ai-story/backend/internal/ports/promptsetrepo"
	"golang.org/x/text/unicode/norm"
)

var (
	ErrIdempotencyConflict = errors.New("idempotency key was already used for a different action")
	ErrActiveHeadConflict  = errors.New("a different action is already generating from this timeline head")
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
	action.Text = strings.TrimSpace(action.Text)
	actionHash := hashPlayerAction(action.Text)
	if key != "" {
		if existing, ok, err := r.Queue.FindByRequest(ctx, id.ID(action.TimelineID), key); err != nil {
			return "", err
		} else if ok {
			if existing.ActionHash != actionHash {
				return "", ErrIdempotencyConflict
			}
			return existing.ID, nil
		}
	}
	if existing, ok, err := r.Queue.FindActiveByTimelineHead(ctx, id.ID(action.TimelineID), action.ExpectedHead); err != nil {
		return "", err
	} else if ok {
		if existing.ActionHash != actionHash {
			return "", ErrActiveHeadConflict
		}
		return existing.ID, nil
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
	job.ActionHash = actionHash
	attempts := r.MaxAttempts
	if attempts < 1 {
		attempts = 3
	}
	if err = r.Queue.Enqueue(ctx, job, attempts); err != nil {
		if key != "" {
			if existing, ok, findErr := r.Queue.FindByRequest(ctx, id.ID(action.TimelineID), key); findErr == nil && ok {
				if existing.ActionHash != actionHash {
					return "", ErrIdempotencyConflict
				}
				return existing.ID, nil
			}
		}
		// The partial unique index closes the race between the lookup above and
		// concurrent inserts. Return the already active job so every caller
		// observes one generation id for one timeline head.
		if existing, ok, findErr := r.Queue.FindActiveByTimelineHead(ctx, id.ID(action.TimelineID), action.ExpectedHead); findErr == nil && ok {
			if existing.ActionHash != actionHash {
				return "", ErrActiveHeadConflict
			}
			return existing.ID, nil
		}
		return "", err
	}
	return gid, nil
}

func hashPlayerAction(text string) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(norm.NFC.String(text)), " "))
	return fmt.Sprintf("%x", sha256.Sum256([]byte(normalized)))
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
	Config      aiconfigrepo.Repository
	Prompts     promptsetrepo.Repository
	Resolve     PipelineResolver
	Hub         *Hub
	Sink        Sink
	Attempts    generationattempts.Store
	RunMetrics  generationmetrics.Store
	Logger      *slog.Logger
	ContextMode string
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
	if e.Attempts != nil {
		contextMode := e.ContextMode
		if contextMode == "" {
			contextMode = "lossless-dedupe-v1"
		}
		pipeline.LLM = &observingLLM{base: pipeline.LLM, store: e.Attempts, logger: e.Logger, jobID: claimed.Job.ID, attemptNo: claimed.Reliability.AttemptCount, contextMode: contextMode}
	}
	if e.RunMetrics != nil {
		contextMode := e.ContextMode
		if contextMode == "" {
			contextMode = "lossless-dedupe-v1"
		}
		queueWait := time.Since(claimed.Job.CreatedAt).Milliseconds()
		if queueWait < 0 {
			queueWait = 0
		}
		pipeline.Metrics = func(metrics generationmetrics.Metrics) {
			metrics.GenerationJobID = claimed.Job.ID
			metrics.AttemptNo = claimed.Reliability.AttemptCount
			metrics.ConfigRevisionID = claimed.Job.ConfigRevisionID
			metrics.PromptSetRevisionID = claimed.Job.PromptSetRevisionID
			metrics.ContextMode = contextMode
			metrics.QueueWaitMS = queueWait
			recordCtx, cancelRecord := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancelRecord()
			if recordErr := e.RunMetrics.Record(recordCtx, metrics); recordErr != nil && e.Logger != nil {
				e.Logger.Warn("generation run telemetry write failed", "generation_id", claimed.Job.ID, "error", recordErr)
			}
		}
	}
	if e.Sink != nil {
		pipeline.Sink = attemptAwareSink{Sink: e.Sink, retryRemaining: claimed.Reliability.AttemptCount < claimed.Reliability.MaxAttempts}
	} else {
		pipeline.Sink = attemptAwareSink{Sink: e.Hub, retryRemaining: claimed.Reliability.AttemptCount < claimed.Reliability.MaxAttempts}
	}
	_, err = pipeline.Run(ctx, claimed.Job.ID, action)
	return err
}
