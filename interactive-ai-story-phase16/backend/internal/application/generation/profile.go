package generation

import (
	domain "github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

// PlannerForProfile resolves the pinned AI revision without changing routing,
// sampling, validation, evaluation dependencies or the atomic Canon commit.
// Quality retains the existing launcher switch, including already-enabled plans.
func PlannerForProfile(profile domain.GenerationProfile, legacyPlanner bool, llm ai.StoryLLM) (TurnPlanner, error) {
	if !profile.Available() {
		return nil, domain.ErrInvalidPublicSettings
	}
	if legacyPlanner || profile.Effective() == domain.GenerationBalance {
		return LLMTurnPlanner{LLM: llm}, nil
	}
	return nil, nil
}
