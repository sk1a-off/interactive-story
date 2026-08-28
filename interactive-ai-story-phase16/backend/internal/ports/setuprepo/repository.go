package setuprepo

import (
	"context"
	"errors"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/setup"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"time"
)

var ErrGenerationActive = errors.New("story setup generation is already running")

type Repository interface {
	CreateStory(context.Context, story.Story, []string) error
	GetStory(context.Context, story.ID) (story.Story, error)
	ListStories(context.Context, story.UserID) ([]StoryOverview, error)
	DeleteStory(context.Context, story.ID, story.UserID) (bool, error)
	ListComponents(context.Context, story.ID) ([]setup.Component, error)
	UpsertComponents(context.Context, []setup.Component) error
	SetComponent(context.Context, setup.Component) error
	CreateSetupGeneration(context.Context, event.GenerationID, story.ID, string, string, string) error
	FinishSetupGeneration(context.Context, event.GenerationID, string, *string) error
	StartStory(context.Context, story.ID, timeline.Timeline, StartMaterialization) error
}
type IDGenerator interface{ New() (id.ID, error) }

type StoryOverview struct {
	Story           story.Story
	ReadyComponents int
	LatestTimeline  *TimelineOverview
	LastActivityAt  time.Time
}

type TimelineOverview struct {
	ID           timeline.ID
	Name         string
	Status       timeline.Status
	HeadEventSeq int64
}

type StartMaterialization struct {
	ChapterID    id.ID
	SceneID      id.ID
	BeatID       id.ID
	ChapterTitle string
	ChapterGoal  string
	SceneGoal    string
	OpeningText  string
	Choices      []string
	Quests       []InitialQuest
	Characters   []InitialCharacter
	Locations    []InitialLocation
	WorldSystems []InitialWorldSystem
	WorldRules   []InitialWorldRule
}

type InitialCharacter struct {
	Kind, Name, Role, Personality, Relationship, VisualAnchorEn string
	Mood, CurrentGoal                                           string
	Age                                                         int
}

type InitialLocation struct {
	Name, Description, VisualAnchorEn string
}

type InitialWorldSystem struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Kind        string                 `json:"kind"`
	Description string                 `json:"description"`
	Resources   []InitialWorldResource `json:"resources"`
}
type InitialWorldResource struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Unit         string   `json:"unit"`
	OwnerScope   string   `json:"ownerScope"`
	InitialValue float64  `json:"initialValue"`
	MinValue     *float64 `json:"minValue,omitempty"`
	MaxValue     *float64 `json:"maxValue,omitempty"`
}
type InitialWorldRule struct {
	ID, SystemID, Title, Category, Severity, Statement string
	Preconditions, Costs, ForbiddenResults             []string
	Exceptions, Tags                                   []string
	Visibility, Status, ExceptionOf                    string
}

type InitialQuest struct {
	QuestType       string
	Title           string
	Description     string
	SuccessCriteria string
	Stages          []InitialQuestStage
}

type InitialQuestStage struct {
	Kind            string
	Title           string
	Description     string
	SuccessCriteria string
}
