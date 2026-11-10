package generation

import (
	"context"
	"log/slog"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

// terminalAppendTimeout bounds the detached write of a terminal update.
const terminalAppendTimeout = 5 * time.Second

type UpdateStore interface {
	Append(context.Context, Update) (Update, error)
	List(context.Context, id.ID, int64) ([]Update, error)
}
type PersistedSink struct {
	Store UpdateStore
	Live  *Hub
	// Logger is optional; when set, failed durable appends are logged.
	Logger *slog.Logger
}

// isTerminalPhase reports phases that end an attempt. They must reach durable
// replay even when the run context is already cancelled or timed out.
func isTerminalPhase(p Phase) bool {
	return p == PhaseFailed || p == PhaseCompleted || p == PhaseRetrying
}

func (s PersistedSink) Publish(ctx context.Context, u Update) {
	if s.Store != nil && u.Phase != PhaseDraftStreaming {
		appendCtx := ctx
		if isTerminalPhase(u.Phase) {
			var cancel context.CancelFunc
			appendCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), terminalAppendTimeout)
			defer cancel()
		}
		stored, e := s.Store.Append(appendCtx, u)
		if e == nil {
			u = stored
		} else if s.Logger != nil {
			s.Logger.Error("generation update persist failed", "generation_id", u.GenerationID, "phase", u.Phase, "error", e)
		}
	}
	if s.Live != nil {
		s.Live.Publish(ctx, u)
	}
}
