package generation

import (
	"context"
	"errors"
	"sync"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

var ErrDuplicateSubmission = errors.New("generation already submitted for request key")

type PipelineFactory func(context.Context) (Pipeline, error)

type Runner struct {
	Pipeline Pipeline
	Factory  PipelineFactory
	Hub      *Hub
	mu       sync.Mutex
	byKey    map[string]id.ID
}

func NewRunner(p Pipeline, h *Hub) *Runner {
	p.Sink = h
	return &Runner{Pipeline: p, Hub: h, byKey: map[string]id.ID{}}
}
func NewRunnerWithFactory(factory PipelineFactory, h *Hub) *Runner {
	return &Runner{Factory: factory, Hub: h, byKey: map[string]id.ID{}}
}

func (r *Runner) Submit(ctx context.Context, key string, a PlayerAction) (id.ID, error) {
	r.mu.Lock()
	if key != "" {
		if existing, ok := r.byKey[key]; ok {
			r.mu.Unlock()
			return existing, nil
		}
	}
	g, err := id.New()
	if err != nil {
		r.mu.Unlock()
		return "", err
	}
	if key != "" {
		r.byKey[key] = g
	}
	r.mu.Unlock()
	pipeline := r.Pipeline
	if r.Factory != nil {
		var err error
		pipeline, err = r.Factory(ctx)
		if err != nil {
			r.mu.Lock()
			if key != "" {
				delete(r.byKey, key)
			}
			r.mu.Unlock()
			return "", err
		}
	}
	pipeline.Sink = r.Hub
	// The pipeline/provider snapshot is captured before the goroutine starts.
	// Changing active provider configuration afterwards affects only future submissions.
	go func(p Pipeline) { _, _ = p.Run(context.WithoutCancel(ctx), g, a) }(pipeline)
	return g, nil
}
func (r *Runner) Subscribe(g id.ID) (<-chan Update, func()) { return r.Hub.Subscribe(g, 32) }

func (r *Runner) Cancel(context.Context, id.ID) error {
	return errors.New("process-local runner cancellation is not durable")
}

func (r *Runner) Replay(context.Context, id.ID, int64) ([]Update, error) { return nil, nil }
