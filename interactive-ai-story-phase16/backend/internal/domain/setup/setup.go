package setup

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
)

type ComponentKey string

const (
	StoryBible       ComponentKey = "story_bible"
	Player           ComponentKey = "player"
	World            ComponentKey = "world"
	WorldRules       ComponentKey = "world_rules"
	InitialCast      ComponentKey = "initial_cast"
	VisualBible      ComponentKey = "visual_bible"
	InitialQuests    ComponentKey = "initial_quests"
	OpeningSituation ComponentKey = "opening_situation"
)

var AllKeys = []ComponentKey{StoryBible, Player, World, WorldRules, InitialCast, VisualBible, InitialQuests, OpeningSituation}
var (
	ErrInvalidComponent = errors.New("invalid story setup component")
	ErrLocked           = errors.New("story setup component is locked")
	ErrNotReady         = errors.New("story setup is not ready")
)

type Component struct {
	StoryID      story.ID            `json:"storyId"`
	Key          ComponentKey        `json:"key"`
	Revision     int                 `json:"revision"`
	Source       string              `json:"source"`
	Payload      json.RawMessage     `json:"payload"`
	Locked       bool                `json:"locked"`
	Status       string              `json:"status"`
	GenerationID *event.GenerationID `json:"generationId,omitempty"`
	UpdatedAt    time.Time           `json:"updatedAt"`
}

func (k ComponentKey) Valid() bool {
	for _, x := range AllKeys {
		if k == x {
			return true
		}
	}
	return false
}
func (c Component) Validate() error {
	if id.ID(c.StoryID).IsZero() || !c.Key.Valid() || c.Revision < 1 || !json.Valid(c.Payload) || len(c.Payload) == 0 {
		return ErrInvalidComponent
	}
	if c.Source != "manual" && c.Source != "ai" && c.Source != "mixed" {
		return ErrInvalidComponent
	}
	if c.Status != "draft" && c.Status != "ready" && c.Status != "invalid" {
		return ErrInvalidComponent
	}
	return nil
}
func ParseKey(v string) (ComponentKey, error) {
	k := ComponentKey(strings.TrimSpace(v))
	if !k.Valid() {
		return "", ErrInvalidComponent
	}
	return k, nil
}
