package generation

import (
	"context"
	"encoding/json"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

// One semantic repair only; neither Writer nor generic syntax repair runs here.
// Domain validation still follows in the pipeline before any event is formed.
func repairObjectiveEvidence(ctx context.Context, llm aiport.StoryLLM, beat string, current []generationtarget.Objective, rejected []objectiveChange) ([]objectiveChange, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	input, err := json.Marshal(map[string]any{
		"newBeat": beat, "currentObjectives": current, "rejectedChanges": rejected,
		"repairInstruction": "The evidence gate rejected the proposed changes: evidenceQuote must be an exact contiguous quote from newBeat; completion additionally requires explicit support for the current successCriteria and a shared meaningful word stem. Return the full changes object again. Select a different direct quote if the criterion is actually satisfied. Never invent, paraphrase or join excerpts with ellipses. Never alter the criterion to pass validation. Remove unsupported changes; do not complete a goal from plans, attempts or partial success.",
	})
	if err != nil {
		return nil, err
	}
	response, err := llm.Generate(ctx, aiport.StoryRequest{Role: "quest_evaluator", PromptVersion: "v1", Input: input, Repair: true})
	if err != nil {
		return nil, err
	}
	var result struct {
		Changes []objectiveChange `json:"changes"`
	}
	if json.Unmarshal(response.Output, &result) != nil {
		return nil, ErrMalformedProposal
	}
	return prepareObjectiveEvidence(beat, current, result.Changes)
}
