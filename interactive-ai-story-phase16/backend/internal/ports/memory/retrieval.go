package memory

import (
	"context"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
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
}
type Retriever interface {
	// Search implementations must apply story/timeline/owner/validity predicates
	// before ranking candidate rows by vector distance.
	Search(context.Context, Query) ([]Memory, error)
}
