package memory

import (
	"errors"
	"fmt"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

var ErrInvalidVisibility = errors.New("invalid memory visibility")

// LineageNode must be loaded from persisted timeline/save-point lineage, never
// accepted from a search client's request. ForkEventSeq is the saved parent head.
type LineageNode struct {
	StoryID          id.ID
	TimelineID       timeline.ID
	ParentTimelineID *timeline.ID
	ForkEventSeq     int64
	HeadEventSeq     int64
}

type HistorySegment struct {
	TimelineID    timeline.ID
	AfterEventSeq int64 // exclusive: a child's cloned base is not a local event
	AtEventSeq    int64 // inclusive, clipped at every edge on the ancestor path
}

// Visibility is immutable after construction. Its slices are not exposed to
// callers, so a ranking adapter cannot accidentally broaden a trusted plan.
type Visibility struct {
	storyID    id.ID
	timelineID timeline.ID
	atEventSeq int64
	baseSeq    int64
	segments   []HistorySegment
}

func ResolveVisibility(storyID id.ID, requested timeline.ID, at int64, nodes []LineageNode) (Visibility, error) {
	invalid := func(reason string) (Visibility, error) {
		return Visibility{}, fmt.Errorf("%w: %s", ErrInvalidVisibility, reason)
	}
	if storyID.IsZero() || id.ID(requested).IsZero() || at < 0 {
		return invalid("missing scope or negative sequence")
	}
	byID := make(map[timeline.ID]LineageNode, len(nodes))
	for _, node := range nodes {
		if _, exists := byID[node.TimelineID]; exists {
			return invalid("duplicate timeline in lineage")
		}
		byID[node.TimelineID] = node
	}
	plan := Visibility{storyID: storyID, timelineID: requested, atEventSeq: at}
	seen := map[timeline.ID]bool{}
	current, cap := requested, at
	for {
		if seen[current] {
			return invalid("cycle in lineage")
		}
		seen[current] = true
		node, exists := byID[current]
		if !exists || node.StoryID != storyID || id.ID(current).IsZero() {
			return invalid("missing or foreign timeline")
		}
		if node.ForkEventSeq < 0 || node.HeadEventSeq < node.ForkEventSeq || cap < node.ForkEventSeq || cap > node.HeadEventSeq {
			return invalid("cursor outside timeline lifetime")
		}
		if current == requested {
			plan.baseSeq = node.ForkEventSeq
		}
		plan.segments = append(plan.segments, HistorySegment{TimelineID: current, AfterEventSeq: node.ForkEventSeq, AtEventSeq: cap})
		if node.ParentTimelineID == nil {
			if node.ForkEventSeq != 0 {
				return invalid("root has a fork boundary")
			}
			return plan, nil
		}
		// cap >= this node's fork boundary was checked above. Crossing its
		// parent edge freezes the ancestor at that boundary, not its live head.
		cap = min(cap, node.ForkEventSeq)
		current = *node.ParentTimelineID
	}
}

func (v Visibility) Segments() []HistorySegment {
	return append([]HistorySegment(nil), v.segments...)
}

func (v Visibility) AllowsHistorical(storyID id.ID, source timeline.ID, eventSeq int64) bool {
	if v.storyID.IsZero() || storyID != v.storyID {
		return false
	}
	for _, segment := range v.segments {
		if source == segment.TimelineID {
			return eventSeq > segment.AfterEventSeq && eventSeq <= segment.AtEventSeq
		}
	}
	return false
}

// AllowsBranchState evaluates inclusive version validity only in the requested
// branch. Baseline snapshot rows start at the branch's creation boundary.
func (v Visibility) AllowsBranchState(storyID id.ID, source timeline.ID, validFrom int64, validTo *int64) bool {
	if v.storyID.IsZero() || storyID != v.storyID || source != v.timelineID || validFrom < v.baseSeq || validFrom > v.atEventSeq {
		return false
	}
	return validTo == nil || (*validTo >= validFrom && *validTo >= v.atEventSeq)
}
