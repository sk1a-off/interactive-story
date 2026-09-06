package generation

import (
	"context"
	"encoding/json"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type CanonExtraction struct {
	WorldChanges     []worldChange     `json:"worldChanges"`
	JournalChanges   []journalChange   `json:"journalChanges"`
	ObjectiveChanges []objectiveChange `json:"objectiveChanges"`
}

type CanonExtractor interface {
	Extract(context.Context, PlayerAction, generationtarget.Target, string, string, direction, pacingDecision) (CanonExtraction, error)
}

// LLMCanonExtractor is benchmark-only until Gate 7 passes. Its output still
// goes through the same evidence and domain validators as legacy evaluators.
type LLMCanonExtractor struct {
	LLM        aiport.StoryLLM
	MaxRepairs int
}

func (e LLMCanonExtractor) Extract(ctx context.Context, action PlayerAction, target generationtarget.Target, intent, beat string, dir direction, pacing pacingDecision) (CanonExtraction, error) {
	payload := map[string]any{
		"action": action.Text, "intent": intent, "directorGoal": dir.Goal,
		"newBeat": beat, "pacing": pacing,
		"currentCharacters": target.InitialCast, "currentWorld": target.World,
		"currentJournal": target.Journal, "currentObjectives": target.Objectives,
		"quests":  questHierarchy(target.Objectives),
		"context": contextWithout(target, "initialCast", "world", "heroJournal", "objectives"),
	}
	var lastErr error
	for attempt := 0; attempt <= e.MaxRepairs; attempt++ {
		input, err := json.Marshal(payload)
		if err != nil {
			return CanonExtraction{}, err
		}
		response, err := e.LLM.Generate(ctx, aiport.StoryRequest{Role: "canon_extractor", PromptVersion: "v1", Input: input, MaxTokens: 2600, Repair: attempt > 0})
		if err != nil {
			return CanonExtraction{}, err
		}
		extraction, validationErr := validateCanonExtraction(beat, target, response.Output)
		if validationErr == nil {
			return extraction, nil
		}
		lastErr = validationErr
		payload["invalidOutput"] = json.RawMessage(append([]byte(nil), response.Output...))
		payload["repairInstruction"] = "Return the full object again. Every mutation must use evidenceQuote, never evidence, copied verbatim without ellipses from newBeat. Remove unsupported mutations."
	}
	return CanonExtraction{}, lastErr
}

func validateCanonExtraction(beat string, target generationtarget.Target, output []byte) (CanonExtraction, error) {
	var extraction CanonExtraction
	if json.Unmarshal(output, &extraction) != nil {
		return CanonExtraction{}, ErrMalformedProposal
	}
	if len(extraction.WorldChanges) > 4 || len(extraction.JournalChanges) > 8 || len(extraction.ObjectiveChanges) > 6 {
		return CanonExtraction{}, ErrRejectedProposal
	}
	var err error
	extraction.WorldChanges, err = prepareWorldEvidence(beat, extraction.WorldChanges)
	if err != nil {
		return CanonExtraction{}, err
	}
	extraction.JournalChanges, err = prepareJournalEvidence(beat, target.Journal, extraction.JournalChanges)
	if err != nil {
		return CanonExtraction{}, err
	}
	extraction.ObjectiveChanges, err = prepareObjectiveEvidence(beat, target.Objectives, extraction.ObjectiveChanges)
	if err != nil {
		return CanonExtraction{}, err
	}
	return extraction, nil
}
