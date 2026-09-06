package generation

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type TurnPlan struct {
	Intent        string `json:"intent"`
	ImmediateGoal string `json:"immediateGoal"`
	Transition    string `json:"transition"`
	Scene         struct {
		Goal       string `json:"goal,omitempty"`
		Mood       string `json:"mood,omitempty"`
		LocationID string `json:"locationId,omitempty"`
	} `json:"scene,omitempty"`
	Chapter struct {
		Title string `json:"title,omitempty"`
		Goal  string `json:"goal,omitempty"`
		Tone  string `json:"tone,omitempty"`
	} `json:"chapter,omitempty"`
	RiskFlags []string `json:"riskFlags"`
}

type TurnPlanner interface {
	Plan(context.Context, PlayerAction, generationtarget.Target, []narrative.DirectorInstruction) (TurnPlan, error)
}

// LLMTurnPlanner is benchmark-only until Gate 5 passes. Go pacing hard limits
// and location validation remain authoritative in Pipeline.
type LLMTurnPlanner struct{ LLM aiport.StoryLLM }

func (p LLMTurnPlanner) Plan(ctx context.Context, action PlayerAction, target generationtarget.Target, instructions []narrative.DirectorInstruction) (TurnPlan, error) {
	input, err := json.Marshal(map[string]any{
		"action": action.Text, "directorInstructions": instructions,
		"quests": questHierarchy(target.Objectives), "heroJournal": target.Journal,
		"context": contextWithout(target, "objectives", "heroJournal"),
	})
	if err != nil {
		return TurnPlan{}, err
	}
	response, err := p.LLM.Generate(ctx, aiport.StoryRequest{Role: "turn_planner", PromptVersion: "v1", Input: input, MaxTokens: 1600})
	if err != nil {
		return TurnPlan{}, err
	}
	var plan TurnPlan
	if json.Unmarshal(response.Output, &plan) != nil {
		return TurnPlan{}, ErrMalformedProposal
	}
	plan.Intent = strings.TrimSpace(plan.Intent)
	plan.ImmediateGoal = strings.TrimSpace(plan.ImmediateGoal)
	if err := validateTurnPlan(action.Text, plan); err != nil {
		return TurnPlan{}, err
	}
	return plan, nil
}

func validateTurnPlan(action string, plan TurnPlan) error {
	if plan.Intent == "" || plan.ImmediateGoal == "" {
		return ErrMalformedProposal
	}
	switch strings.ToLower(strings.TrimSpace(plan.Transition)) {
	case "continue_scene", "new_scene", "new_chapter":
	default:
		return ErrMalformedProposal
	}
	if !intentPreservesAction(action, plan.Intent) {
		return ErrRejectedProposal
	}
	return nil
}

func intentPreservesAction(action, intent string) bool {
	actionStems := meaningfulStems(action)
	intentStems := meaningfulStems(intent)
	matched := 0
	for stem := range actionStems {
		if _, ok := intentStems[stem]; ok {
			matched++
		}
	}
	if len(actionStems) > 0 && matched*3 < len(actionStems) {
		return false
	}
	return true
}

func pacingFromTurnPlan(plan TurnPlan) pacingDecision {
	return pacingDecision{
		Transition: plan.Transition, ChapterTitle: plan.Chapter.Title,
		ChapterGoal: plan.Chapter.Goal, ChapterTone: plan.Chapter.Tone,
		SceneGoal: plan.Scene.Goal, SceneMood: plan.Scene.Mood, LocationID: plan.Scene.LocationID,
	}
}
