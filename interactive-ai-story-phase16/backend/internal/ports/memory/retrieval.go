package memory

import (
	"context"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/memory"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type OwnerType string

const (
	OwnerStory     OwnerType = "story"
	OwnerCharacter OwnerType = "character"
	OwnerTimeline  OwnerType = "timeline"
)

type Query struct {
	StoryID    id.ID
	TimelineID timeline.ID
	OwnerType  OwnerType
	OwnerID    id.ID
	AtEventSeq int64
	Vector     []float32
	Limit      int
	// Hybrid retrieval requires an explicit embedding identity. Legacy exact
	// retrieval remains available for callers that have not migrated yet.
	Text    string
	Profile EmbeddingProfile
}
type EmbeddingProfile struct {
	Provider string
	Model    string
	Revision int64
}
type Memory struct {
	ID           id.ID
	StoryID      id.ID
	TimelineID   timeline.ID
	OwnerType    OwnerType
	OwnerID      id.ID
	ValidFromSeq int64
	ValidToSeq   *int64
	Kind         string
	Content      string
	Score        float64
	// TimelineID always identifies the original source, including ancestors.
	DocumentClass string
	SourceKey     string
	Profile       EmbeddingProfile
}

// VisibilityResolver loads trusted persisted lineage independently of rankings.
type VisibilityResolver interface {
	Resolve(context.Context, id.ID, timeline.ID, int64) (domain.Visibility, error)
}
type Retriever interface {
	// Search implementations must apply story, owner, temporal validity, and
	// trusted branch visibility before ranking. Ancestors require fork caps;
	// returned TimelineID/OwnerID must preserve the original source scope.
	Search(context.Context, Query) ([]Memory, error)
}
