package readerrepo

import (
	"context"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type Beat struct {
	ID         id.ID    `json:"id"`
	Position   int      `json:"position"`
	Text       string   `json:"text"`
	Paragraphs []string `json:"paragraphs"`
}
type SceneImage struct {
	ID      id.ID  `json:"id"`
	Variant int    `json:"variant"`
	URL     string `json:"url"`
}
type ImageGeneration struct {
	ID              id.ID        `json:"id"`
	SceneID         id.ID        `json:"sceneId"`
	SourceBeatID    *id.ID       `json:"sourceBeatId,omitempty"`
	MomentIndex     int          `json:"momentIndex"`
	AnchorParagraph int          `json:"anchorParagraph,omitempty"`
	Status          string       `json:"status"`
	Images          []SceneImage `json:"images"`
	SelectedImageID *id.ID       `json:"selectedImageId,omitempty"`
}
type ChoiceSet struct {
	BeatID  id.ID    `json:"beatId"`
	Choices []string `json:"choices"`
}
type Objective struct {
	ID                string `json:"id"`
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
type HeroStat struct {
	Key       string `json:"key"`
	ValueType string `json:"valueType"`
	Value     any    `json:"value"`
	Name      string `json:"name,omitempty"`
}

type Current struct {
	TimelineID       timeline.ID       `json:"timelineId"`
	StoryID          id.ID             `json:"storyId"`
	HeadEventSeq     int64             `json:"headEventSeq"`
	ChapterNumber    int               `json:"chapterNumber"`
	ChapterTitle     string            `json:"chapterTitle"`
	ChapterGoal      string            `json:"chapterGoal"`
	SceneID          id.ID             `json:"sceneId"`
	SceneNumber      int               `json:"sceneNumber"`
	SceneGoal        string            `json:"sceneGoal"`
	BeatID           id.ID             `json:"beatId"`
	Text             string            `json:"text"` // latest beat, compatibility
	Beats            []Beat            `json:"beats"`
	Choices          []string          `json:"choices"` // compatibility mirror of ChoiceSet.Choices
	ChoiceSet        *ChoiceSet        `json:"choiceSet,omitempty"`
	Objectives       []Objective       `json:"objectives"`
	Abilities        []JournalEntry    `json:"abilities"`
	Attributes       []JournalEntry    `json:"attributes"`
	Inventory        []JournalEntry    `json:"inventory"`
	Currencies       []JournalEntry    `json:"currencies"`
	HeroStats        []HeroStat        `json:"heroStats"`
	ImageGeneration  *ImageGeneration  `json:"imageGeneration,omitempty"` // latest, compatibility
	ImageGenerations []ImageGeneration `json:"imageGenerations,omitempty"`
}
type Repository interface {
	Current(context.Context, timeline.ID) (Current, error)
}
