package promptset

import (
	"errors"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

var ErrInvalid = errors.New("invalid prompt set")

var Roles = []string{
	"story_setup",
	"setup_architect",
	"setup_assistant",
	"setup_editor",
	"director_editor",
	"action_interpreter",
	"director",
	"pacing",
	"writer",
	"lore_guard",
	"world_evaluator",
	"state_evaluator",
	"objectives",
	"quest_evaluator",
	"choices",
	"image_prompt",
	"image_moments",
	"image_next_moment",
	"image_paragraph",
	"structured_repair",
}

type RoleSettings struct {
	Temperature float64 `json:"temperature"`
	TopP        float64 `json:"topP"`
	MaxTokens   int     `json:"maxTokens"`
}

func DefaultRoleSettings(role string) RoleSettings {
	switch role {
	case "writer":
		return RoleSettings{Temperature: 0.8, TopP: 0.95, MaxTokens: 4096}
	case "choices":
		return RoleSettings{Temperature: 0.6, TopP: 0.9, MaxTokens: 1024}
	case "director", "pacing", "objectives", "quest_evaluator":
		return RoleSettings{Temperature: 0.5, TopP: 0.9, MaxTokens: 2048}
	case "state_evaluator", "world_evaluator", "lore_guard":
		return RoleSettings{Temperature: 0.25, TopP: 0.8, MaxTokens: 1400}
	case "image_prompt", "image_moments", "image_next_moment", "image_paragraph":
		return RoleSettings{Temperature: 0.5, TopP: 0.9, MaxTokens: 2048}
	case "story_setup", "setup_architect":
		return RoleSettings{Temperature: 0.7, TopP: 0.8, MaxTokens: 8192}
	case "setup_assistant", "setup_editor", "director_editor":
		return RoleSettings{Temperature: 0.45, TopP: 0.85, MaxTokens: 8192}
	default:
		return RoleSettings{Temperature: 0.2, TopP: 0.8, MaxTokens: 2048}
	}
}

func (s RoleSettings) Validate() error {
	if s.Temperature < 0 || s.Temperature > 2 || s.TopP <= 0 || s.TopP > 1 || s.MaxTokens < 128 || s.MaxTokens > 32768 {
		return ErrInvalid
	}
	return nil
}

type Revision struct {
	ID           id.ID                   `json:"-"`
	Revision     int64                   `json:"-"`
	Prompts      map[string]string       `json:"-"`
	RoleSettings map[string]RoleSettings `json:"-"`
}

type CreateCommand struct {
	Prompts      map[string]string       `json:"prompts"`
	RoleSettings map[string]RoleSettings `json:"roleSettings"`
}

func (c CreateCommand) Validate() error {
	if len(c.Prompts) != len(Roles) || len(c.RoleSettings) != len(Roles) {
		return ErrInvalid
	}
	for _, role := range Roles {
		if strings.TrimSpace(c.Prompts[role]) == "" {
			return ErrInvalid
		}
		if err := c.RoleSettings[role].Validate(); err != nil {
			return err
		}
	}
	return nil
}

type SafeView struct {
	ID           string                  `json:"id"`
	Revision     int64                   `json:"revision"`
	Prompts      map[string]string       `json:"prompts"`
	RoleSettings map[string]RoleSettings `json:"roleSettings"`
	Active       bool                    `json:"active"`
}
