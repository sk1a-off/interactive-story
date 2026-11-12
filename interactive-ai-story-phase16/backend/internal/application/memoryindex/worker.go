package memoryindex

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/memoryindexrepo"
)

type Worker struct {
	Queue            repo.Queue
	Store            repo.Store
	Embeddings       ai.EmbeddingProvider
	Profile          repo.Profile
	Owner            string
	LeaseTTL         time.Duration
	HeartbeatEvery   time.Duration
	ExecutionTimeout time.Duration
	Now              func() time.Time
}

// Clock returns the worker's notion of now (UTC).
func (w Worker) Clock() time.Time { return w.now() }

func (w Worker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

func (w Worker) RunOnce(ctx context.Context) error {
	if w.Queue == nil || w.Store == nil || w.Embeddings == nil || w.Owner == "" || w.Profile.Revision < 1 {
		return errors.New("memory indexing worker not configured")
	}
	identity := w.Embeddings.Identity()
	if w.Embeddings.Dimensions() != 384 || identity.Kind != ai.KindEmbedding || identity.Provider != w.Profile.Provider || identity.Model != w.Profile.Model {
		return errors.New("memory embedding profile mismatch")
	}
	ttl := w.LeaseTTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	heartbeat := w.HeartbeatEvery
	if heartbeat <= 0 {
		heartbeat = ttl / 3
	}
	if heartbeat <= 0 || heartbeat >= ttl {
		return errors.New("memory heartbeat must be shorter than lease TTL")
	}
	job, err := w.Queue.Claim(ctx, w.Owner, w.now(), ttl)
	if err != nil {
		return err
	}
	timeout := w.ExecutionTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	done := make(chan struct{})
	heartbeatError := make(chan error, 1)
	go func() {
		defer close(done)
		ticker := time.NewTicker(heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if err := w.Queue.Heartbeat(runCtx, job, w.now(), ttl); err != nil {
					heartbeatError <- err
					cancel()
					return
				}
			}
		}
	}()
	err = w.execute(runCtx, job)
	cancel()
	<-done
	select {
	case heartbeatErr := <-heartbeatError:
		return fmt.Errorf("memory indexing heartbeat: %w", heartbeatErr)
	default:
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, repo.ErrLeaseLost) || ctx.Err() != nil {
		return err
	}
	finalCtx, finalCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finalCancel()
	backoff := time.Second << min(max(job.Attempts-1, 0), 5)
	// Provider errors can contain source text/URLs. Store a generic diagnostic;
	// typed error detail remains available to the supervising caller.
	if retryErr := w.Queue.Retry(finalCtx, job, w.now(), backoff, "memory indexing failed"); retryErr != nil {
		return retryErr
	}
	return fmt.Errorf("memory indexing attempt: %w", err)
}

func (w Worker) execute(ctx context.Context, job repo.Job) error {
	documents, err := w.Store.Load(ctx, job)
	if err != nil {
		return err
	}
	changed, err := w.Store.Changed(ctx, job, w.Profile, documents)
	if err != nil {
		return err
	}
	vectors := make(map[string][]float32, len(changed))
	for offset := 0; offset < len(changed); offset += 64 {
		end := min(offset+64, len(changed))
		texts := make([]string, end-offset)
		for index, document := range changed[offset:end] {
			texts[index] = document.Content
		}
		response, err := w.Embeddings.EmbedDocuments(ctx, ai.EmbeddingRequest{Texts: texts})
		if err != nil {
			return err
		}
		if len(response.Vectors) != len(texts) {
			return ai.ErrInvalidOutput
		}
		for index, vector := range response.Vectors {
			if len(vector) != 384 {
				return ai.ErrInvalidOutput
			}
			norm := float64(0)
			for _, value := range vector {
				if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
					return ai.ErrInvalidOutput
				}
				norm += float64(value) * float64(value)
			}
			if norm == 0 {
				return ai.ErrInvalidOutput
			}
			vectors[changed[offset+index].Key] = vector
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return w.Store.Publish(ctx, job, w.Profile, documents, vectors, w.now())
}

func (w Worker) Run(ctx context.Context, poll time.Duration) error {
	if poll <= 0 {
		poll = time.Second
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := w.RunOnce(ctx)
		if err != nil && !errors.Is(err, repo.ErrNoJob) {
			return err
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
