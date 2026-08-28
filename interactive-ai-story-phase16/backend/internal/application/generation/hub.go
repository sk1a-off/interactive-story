package generation

import (
	"context"
	"sync"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

type Hub struct {
	mu   sync.RWMutex
	subs map[id.ID]map[chan Update]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[id.ID]map[chan Update]struct{}{}} }
func (h *Hub) Publish(_ context.Context, u Update) {
	h.mu.RLock()
	defer h.mu.RUnlock()
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
