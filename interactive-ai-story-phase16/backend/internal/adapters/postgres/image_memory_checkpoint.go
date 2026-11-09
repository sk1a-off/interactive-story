package postgres

import (
	"context"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

// ResolveMemoryCheckpoint is called only when image memory is explicitly enabled.
// Cloned/legacy beats with no local immutable commit return nil, never live head.
func (r *ImageGenerationRepository) ResolveMemoryCheckpoint(ctx context.Context, storyID, timelineID, sceneID, beatID id.ID) (*int64, error) {
	var seq *int64
	err := r.pool.QueryRow(ctx, `SELECT min(e.seq)
 FROM beats b JOIN scenes s ON s.id=b.scene_id JOIN chapters c ON c.id=s.chapter_id
 JOIN timelines t ON t.id=c.timeline_id
 JOIN story_events e ON e.timeline_id=t.id AND e.event_type='beat_committed'
   AND e.payload->>'beatId'=b.id::text AND e.seq<=t.head_event_seq
 WHERE t.story_id=$1 AND t.id=$2 AND s.id=$3 AND b.id=$4`, storyID, timelineID, sceneID, beatID).Scan(&seq)
	return seq, err
}
