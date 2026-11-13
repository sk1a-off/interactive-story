package role_router

import (
	"context"
	"errors"
	"testing"
	"time"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

type stallingLLM struct{ roles []string }

func (l *stallingLLM) Identity() aiport.ProviderIdentity {
	return aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "google_gemini", Model: "fast"}
}

func (l *stallingLLM) Generate(ctx context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	l.roles = append(l.roles, request.Role)
	<-ctx.Done()
	return aiport.StoryResponse{}, ctx.Err()
}

func TestSafeHybridEscalatesStalledFastLegWhileTurnIsLive(t *testing.T) {
	fast := &stallingLLM{}
	quality := &recordingLLM{identity: aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "google_gemini", Model: "quality"}}
	router, err := NewSafeHybrid(fast, quality)
	if err != nil {
		t.Fatal(err)
	}
	router.FastLegTimeout = 20 * time.Millisecond
	response, err := router.Generate(context.Background(), aiport.StoryRequest{Role: "turn_planner"})
	if err != nil {
		t.Fatalf("stalled fast leg should escalate: %v", err)
	}
	if response.EscalatedFrom != "fast" || len(quality.roles) != 1 {
		t.Fatalf("quality model did not answer: %#v quality=%v", response, quality.roles)
	}
	// A cancelled turn must not escalate even though the fast leg stalled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	quality.roles = nil
	if _, err := router.Generate(ctx, aiport.StoryRequest{Role: "choices"}); !errors.Is(err, context.Canceled) || len(quality.roles) != 0 {
		t.Fatalf("cancelled turn escalated: err=%v quality=%v", err, quality.roles)
	}
}
