package imagegeneration

import (
	"context"
	"encoding/json"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/application/memorycontext"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/imagegenerationrepo"
)

func (b LLMPromptBuilder) memoryContext(ctx context.Context, s imagegenerationrepo.SceneSource) *memorycontext.Supplement {
	if b.Memory == nil || b.Memory.TopK <= 0 || b.Memory.MemoryTokenBudget <= 0 {
		return nil
	}
	timeout := b.Memory.Builder.RetrievalTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if s.AtEventSeq == nil && b.MemoryCheckpoints != nil {
		seq, err := b.MemoryCheckpoints.ResolveMemoryCheckpoint(ctx, s.StoryID, s.TimelineID, s.SceneID, s.BeatID)
		if err != nil {
			if b.Memory.Logger != nil {
				b.Memory.Logger.WarnContext(ctx, "image memory checkpoint unavailable", "story_id", s.StoryID, "scene_id", s.SceneID)
			}
			return nil
		}
		s.AtEventSeq = seq
	}
	if s.AtEventSeq == nil {
		return nil
	}
	authoritative, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	return b.Memory.Build(ctx, memorycontext.Request{StoryID: s.StoryID, TimelineID: timeline.ID(s.TimelineID), AtEventSeq: *s.AtEventSeq,
		QueryText: s.Text + "\n" + s.Goal, Authoritative: authoritative, Recent: []string{s.Text}})
}
