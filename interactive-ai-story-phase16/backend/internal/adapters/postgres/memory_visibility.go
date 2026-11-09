package postgres

import (
	"context"
	"fmt"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/memory"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type MemoryVisibilityResolver struct{ q pgxQuerier }

func NewMemoryVisibilityResolver(q pgxQuerier) *MemoryVisibilityResolver {
	return &MemoryVisibilityResolver{q: q}
}

func (r *MemoryVisibilityResolver) Resolve(ctx context.Context, storyID id.ID, requested timeline.ID, at int64) (domain.Visibility, error) {
	// Read all lineage edges and save boundaries in a single statement snapshot.
	// Stop recursion on cycles/depth exhaustion; the domain then rejects the
	// duplicate or missing parent rather than returning a truncated visible path.
	rows, err := r.q.Query(ctx, `WITH RECURSIVE lineage AS (
 SELECT t.id,t.story_id,t.parent_timeline_id,t.forked_from_save_id,t.head_event_seq,
        ARRAY[t.id] AS visited,false AS cycle,0 AS depth
 FROM timelines t WHERE t.id=$1::uuid
 UNION ALL
 SELECT p.id,p.story_id,p.parent_timeline_id,p.forked_from_save_id,p.head_event_seq,
        l.visited || p.id,p.id=ANY(l.visited),l.depth+1
 FROM lineage l JOIN timelines p ON p.id=l.parent_timeline_id
 WHERE NOT l.cycle AND l.depth<255
)
SELECT l.id::text,l.story_id::text,COALESCE(l.parent_timeline_id::text,''),
 CASE WHEN l.parent_timeline_id IS NULL AND l.forked_from_save_id IS NULL THEN 0
      WHEN s.id IS NOT NULL THEN s.event_seq ELSE -1 END,
 l.head_event_seq
FROM lineage l
LEFT JOIN save_points s ON s.id=l.forked_from_save_id
 AND s.timeline_id=l.parent_timeline_id AND s.story_id=l.story_id`, id.ID(requested).String())
	if err != nil {
		return domain.Visibility{}, fmt.Errorf("read memory lineage: %w", err)
	}
	defer rows.Close()
	var nodes []domain.LineageNode
	for rows.Next() {
		var node domain.LineageNode
		var timelineText, storyText, parentText string
		if err = rows.Scan(&timelineText, &storyText, &parentText, &node.ForkEventSeq, &node.HeadEventSeq); err != nil {
			return domain.Visibility{}, err
		}
		timelineID, parseErr := id.Parse(timelineText)
		if parseErr != nil {
			return domain.Visibility{}, parseErr
		}
		node.TimelineID = timeline.ID(timelineID)
		if node.StoryID, err = id.Parse(storyText); err != nil {
			return domain.Visibility{}, err
		}
		if parentText != "" {
			parentID, parseErr := id.Parse(parentText)
			if parseErr != nil {
				return domain.Visibility{}, parseErr
			}
			parent := timeline.ID(parentID)
			node.ParentTimelineID = &parent
		}
		nodes = append(nodes, node)
	}
	if err = rows.Err(); err != nil {
		return domain.Visibility{}, err
	}
	return domain.ResolveVisibility(storyID, requested, at, nodes)
}
