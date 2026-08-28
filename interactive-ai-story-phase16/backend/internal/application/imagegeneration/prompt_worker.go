package imagegeneration

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/imagegenerationrepo"
)

// PromptJobWorker prepares durable image generations after Canon has committed.
// The browser can therefore receive story text and choices without waiting for
// the separate visual-prompt LLM request.
type PromptJobWorker struct {
	Repo             imagegenerationrepo.PromptJobRepository
	Images           Service
	Owner            string
	LocalOwnerID     id.ID
	LeaseTTL         time.Duration
	ExecutionTimeout time.Duration
	Now              func() time.Time
	Logger           *slog.Logger
}

func (w PromptJobWorker) RunOnce(ctx context.Context) error {
	now := w.Now().UTC()
	job, err := w.Repo.ClaimPromptJob(ctx, w.Owner, w.LocalOwnerID, now, w.LeaseTTL)
	if err != nil {
		return err
	}
	runCtx := ctx
	cancel := func() {}
	if w.ExecutionTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, w.ExecutionTimeout)
	}
	err = w.Images.SchedulePromptJob(runCtx, job)
	cancel()
	finalCtx, finalCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finalCancel()
	if err == nil {
		return w.Repo.CompletePromptJob(finalCtx, job.ID, w.Owner, w.Now().UTC())
	}
	shift := job.Attempts - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 5 {
		shift = 5
	}
	backoff := time.Second << shift
	if w.Logger != nil {
		w.Logger.Warn("illustration prompt job failed", "job_id", job.ID, "story_id", job.StoryID, "scene_id", job.SceneID, "attempt", job.Attempts, "error", err)
	}
	return w.Repo.FailPromptJob(finalCtx, job.ID, w.Owner, w.Now().UTC(), backoff, err.Error())
}

func (w PromptJobWorker) Run(ctx context.Context, poll time.Duration) error {
	if w.Repo == nil || w.Owner == "" || w.Now == nil {
		return errors.New("image prompt worker unavailable")
	}
	if poll <= 0 {
		poll = 250 * time.Millisecond
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		err := w.RunOnce(ctx)
		if err != nil && !errors.Is(err, imagegenerationrepo.ErrNoJob) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
