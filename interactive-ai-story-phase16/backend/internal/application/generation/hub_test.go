package generation

import (
	"context"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"testing"
)

func TestHubDoesNotBlockOnSlowSubscriber(t *testing.T) {
	h := NewHub()
	g := id.MustParse("00000000-0000-4000-8000-000000000099")
	_, cancel := h.Subscribe(g, 1)
	defer cancel()
	h.Publish(context.Background(), Update{GenerationID: g, Phase: PhaseQueued})
	h.Publish(context.Background(), Update{GenerationID: g, Phase: PhaseWriting}) // deliberately dropped instead of blocking
}
