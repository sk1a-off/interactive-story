package generation

import (
	"context"
	"encoding/json"

	"github.com/local/interactive-ai-story/backend/internal/application/contextbuilder"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

type ShadowContextResult struct {
	RetrievedCount int
	RetrievalError string
}

type ShadowContext interface {
	Build(context.Context, PlayerAction, generationtarget.Target) (ShadowContextResult, error)
}

// ContextShadowBuilder exercises the future context path without feeding its
// output to any role. Production writer input and Canon remain unchanged.
type ContextShadowBuilder struct {
	Builder        contextbuilder.Builder
	TokenBudget    int
	RetrievalLimit int
}

func (s ContextShadowBuilder) Build(ctx context.Context, action PlayerAction, target generationtarget.Target) (ShadowContextResult, error) {
	authoritative, err := json.Marshal(target)
	if err != nil {
		return ShadowContextResult{}, err
	}
	recent := make([]string, 0, len(target.RecentBeats))
	for _, beat := range target.RecentBeats {
		recent = append(recent, beat.Text)
	}
	result, err := s.Builder.Build(ctx, contextbuilder.Input{
		State: contextbuilder.AuthoritativeState{
			StoryID: id.ID(action.StoryID), TimelineID: action.TimelineID,
			HeadEventSeq: action.ExpectedHead, CurrentState: authoritative, RecentBeats: recent,
		},
		QueryText: action.Text, OwnerType: memory.OwnerTimeline, OwnerID: id.ID(action.TimelineID),
		TokenBudget: s.TokenBudget, RetrievalLimit: s.RetrievalLimit,
	})
	return ShadowContextResult{RetrievedCount: len(result.Retrieved), RetrievalError: result.RetrievalError}, err
}
