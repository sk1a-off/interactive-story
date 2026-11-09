package contextbuilder

import (
	"math"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/memory"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

func validQueryVector(v []float32) bool {
	if len(v) != 384 {
		return false
	}
	norm := float64(0)
	for _, value := range v {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return false
		}
		norm += float64(value) * float64(value)
	}
	return norm > 0
}

func allowsBranchMemory(plan domain.Visibility, profile memory.EmbeddingProfile, in Input, m memory.Memory) bool {
	if m.StoryID != in.State.StoryID || m.OwnerType != in.OwnerType || m.Profile != profile || m.SourceKey == "" || math.IsNaN(m.Score) || math.IsInf(m.Score, 0) {
		return false
	}
	switch in.OwnerType {
	case memory.OwnerTimeline:
		if in.OwnerID != id.ID(in.State.TimelineID) || m.OwnerID != id.ID(m.TimelineID) {
			return false
		}
	case memory.OwnerStory:
		if in.OwnerID != in.State.StoryID || m.OwnerID != in.OwnerID {
			return false
		}
	case memory.OwnerCharacter:
		if in.OwnerID.IsZero() || m.OwnerID != in.OwnerID {
			return false
		}
	default:
		return false
	}
	switch m.DocumentClass {
	case "historical_beat":
		return plan.AllowsHistoricalVersion(m.StoryID, m.TimelineID, m.ValidFromSeq, m.ValidToSeq)
	case "branch_state":
		return plan.AllowsBranchState(m.StoryID, m.TimelineID, m.ValidFromSeq, m.ValidToSeq)
	default:
		return false
	}
}
