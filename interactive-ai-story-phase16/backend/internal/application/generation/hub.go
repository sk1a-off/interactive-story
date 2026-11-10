package generation

import (
	"context"
	"sync"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

type Hub struct {
	mu     sync.RWMutex
	subs   map[id.ID]map[chan Update]struct{}
	drafts map[id.ID]Update
}

func NewHub() *Hub {
	return &Hub{subs: map[id.ID]map[chan Update]struct{}{}, drafts: map[id.ID]Update{}}
}
func (h *Hub) Publish(_ context.Context, u Update) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.drafts == nil {
		h.drafts = map[id.ID]Update{}
	}
	if u.Phase == PhaseDraftStreaming {
		h.drafts[u.GenerationID] = u
	}
	if u.Phase == PhaseDraftReady || u.Phase == PhaseDraftReplaced || u.Phase == PhaseCompleted || u.Phase == PhaseFailed || u.Phase == PhaseRetrying {
		delete(h.drafts, u.GenerationID)
	}
	for ch := range h.subs[u.GenerationID] {
		select {
		case ch <- u:
		default:
		} // a slow browser must never block generation/Canon.
	}
}
func (h *Hub) Subscribe(g id.ID, buffer int) (<-chan Update, func()) {
	if buffer < 1 {
		buffer = 16
	}
	ch := make(chan Update, buffer)
	h.mu.Lock()
	if h.subs[g] == nil {
		h.subs[g] = map[chan Update]struct{}{}
	}
	h.subs[g][ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs[g], ch)
			if len(h.subs[g]) == 0 {
				delete(h.subs, g)
			}
			close(ch)
			h.mu.Unlock()
		})
	}
	return ch, cancel
}

// LatestDraft is process-local replay only; token snapshots never enter Canon or SQL.
func (h *Hub) LatestDraft(g id.ID) (Update, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	u, ok := h.drafts[g]
	return u, ok
}
