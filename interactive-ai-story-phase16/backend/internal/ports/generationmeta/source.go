package generationmeta

import (
	"context"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type Source interface {
	StoryForTimeline(context.Context, timeline.ID) (story.ID, error)
}
