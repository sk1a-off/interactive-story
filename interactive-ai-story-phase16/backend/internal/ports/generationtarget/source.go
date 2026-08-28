package generationtarget

import (
	"context"
	"encoding/json"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type Target struct {
	ChapterID         id.ID           `json:"chapterId"`
	SceneID           id.ID           `json:"sceneId"`
	ChapterNumber     int             `json:"chapterNumber"`
	SceneNumber       int             `json:"sceneNumber"`
	ChapterSceneCount int             `json:"chapterSceneCount"`
	ChapterBeatCount  int             `json:"chapterBeatCount"`
	NextBeatPosition  int             `json:"nextBeatPosition"`
	ChapterTitle      string          `json:"chapterTitle"`
	ChapterGoal       string          `json:"chapterGoal"`
	SceneGoal         string          `json:"sceneGoal"`
	SceneMood         string          `json:"sceneMood"`
	PreviousBeatText  string          `json:"previousBeatText"`
	StoryBible        json.RawMessage `json:"storyBible"`
	Player            json.RawMessage `json:"player"`
	World             json.RawMessage `json:"world"`
	InitialCast       json.RawMessage `json:"initialCast"`
	VisualBible       json.RawMessage `json:"visualBible"`
	Objectives        []Objective     `json:"objectives"`
	Journal           []JournalEntry  `json:"heroJournal"`
	RecentBeats       []RecentBeat    `json:"recentBeats"`
	WorldSystems      []WorldSystem   `json:"worldSystems"`
	WorldRules        []WorldRule     `json:"worldRules"`
	WorldResources    []WorldResource `json:"worldResources"`
}

type WorldSystem struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Description string          `json:"description"`
	Status      string          `json:"status"`
	Resources   json.RawMessage `json:"resources"`
	Version     int64           `json:"version"`
}
type WorldRule struct {
	ID               string   `json:"id"`
	SystemID         string   `json:"systemId"`
	Title            string   `json:"title"`
	Category         string   `json:"category"`
	Severity         string   `json:"severity"`
	Statement        string   `json:"statement"`
	Preconditions    []string `json:"preconditions"`
	Costs            []string `json:"costs"`
	ForbiddenResults []string `json:"forbiddenResults"`
	Exceptions       []string `json:"exceptions"`
	Tags             []string `json:"tags"`
	Visibility       string   `json:"visibility"`
	Status           string   `json:"status"`
	ExceptionOf      string   `json:"exceptionOf,omitempty"`
	Source           string   `json:"source"`
	Evidence         string   `json:"evidence,omitempty"`
	Version          int64    `json:"version"`
}
type WorldResource struct {
	ID         string   `json:"id"`
	SystemID   string   `json:"systemId"`
	OwnerType  string   `json:"ownerType"`
	OwnerID    string   `json:"ownerId,omitempty"`
	OwnerKey   string   `json:"ownerKey,omitempty"`
	Name       string   `json:"name"`
	Unit       string   `json:"unit,omitempty"`
	Current    float64  `json:"currentValue"`
	Minimum    *float64 `json:"minValue,omitempty"`
	Maximum    *float64 `json:"maxValue,omitempty"`
	Visibility string   `json:"visibility"`
	Version    int64    `json:"version"`
}

type RecentBeat struct {
	ChapterNumber int    `json:"chapterNumber"`
	ChapterTitle  string `json:"chapterTitle"`
	SceneNumber   int    `json:"sceneNumber"`
	SceneGoal     string `json:"sceneGoal"`
	Position      int    `json:"position"`
	Text          string `json:"text"`
}
type Objective struct {
	ID                string `json:"id,omitempty"`
	ParentObjectiveID string `json:"parentObjectiveId,omitempty"`
	Scope             string `json:"scope"`
	Kind              string `json:"kind"`
	QuestType         string `json:"questType,omitempty"`
	Title             string `json:"title"`
	Description       string `json:"description,omitempty"`
	SuccessCriteria   string `json:"successCriteria"`
	Status            string `json:"status"`
	Progress          int    `json:"progress"`
	Evidence          string `json:"evidence,omitempty"`
	Virtual           bool   `json:"virtual,omitempty"`
}
type JournalEntry struct {
	ID          string   `json:"id"`
	Category    string   `json:"category"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Quantity    int      `json:"quantity"`
	Level       string   `json:"level,omitempty"`
	Status      string   `json:"status"`
	Evidence    string   `json:"evidence,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}
type Source interface {
	CurrentWriteTarget(context.Context, timeline.ID) (Target, error)
}
type RuleAuditRecord struct {
	RuleID            string
	Severity          string
	Evidence          string
	RepairInstruction string
	Status            string
}
type RuleAuditStore interface {
	RecordRuleAudit(context.Context, timeline.ID, id.ID, []RuleAuditRecord) error
}
