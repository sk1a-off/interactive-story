package generation

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/application/memorycontext"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type memoryTurnPlanner interface {
	PlanWithMemory(context.Context, PlayerAction, generationtarget.Target, []narrative.DirectorInstruction, *memorycontext.Supplement) (TurnPlan, error)
}

func (p Pipeline) memoryContext(ctx context.Context, a PlayerAction, target generationtarget.Target) *memorycontext.Supplement {
	if p.Memory == nil {
		return nil
	}
	authoritative, err := json.Marshal(target)
	if err != nil {
		return nil
	}
	recent := make([]string, 0, len(target.RecentBeats)+1)
	for _, beat := range target.RecentBeats {
		recent = append(recent, beat.Text)
	}
	recent = append(recent, target.PreviousBeatText)
	goal := strings.TrimSpace(target.SceneGoal)
	if goal == "" {
		goal = target.ChapterGoal
	}
	return p.Memory.Build(ctx, memorycontext.Request{StoryID: a.StoryID, TimelineID: a.TimelineID, AtEventSeq: a.ExpectedHead,
		QueryText: a.Text + "\n" + goal, Authoritative: authoritative, Recent: recent})
}
