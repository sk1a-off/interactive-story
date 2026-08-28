package imagegenerationrepo

import (
	"context"
	"errors"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/imagegeneration"
)

var (
	ErrNoJob           = errors.New("no image generation job available")
	ErrAnchorOccupied  = errors.New("illustration anchor is already occupied")
	ErrPromptLeaseLost = errors.New("image prompt job lease lost")
)

type SceneSource struct {
	StoryID, SceneID, TimelineID, BeatID id.ID
	Mood, Goal, Text                     string
	StoryBible, Player, World            string
	InitialCast, VisualBible             string
	BeatPosition                         int
	ExcludedParagraphs                   []int
}
type Repository interface {
	LoadSceneSource(context.Context, id.ID, id.ID) (SceneSource, error)
	LoadSceneSources(context.Context, id.ID, id.ID) ([]SceneSource, error)
	LoadSceneSourceForBeat(context.Context, id.ID, id.ID, id.ID) (SceneSource, error)
	Create(context.Context, imagegeneration.Generation) error
	Get(context.Context, id.ID) (imagegeneration.View, error)
	HasAutomaticMoment(context.Context, id.ID, id.ID, int) (bool, error)
	ClaimNext(context.Context, string, time.Time, time.Duration) (imagegeneration.Generation, error)
	Complete(context.Context, id.ID, string, []imagegeneration.Image, time.Time) (imagegeneration.View, error)
	Fail(context.Context, id.ID, string, time.Time, string, string) (imagegeneration.Generation, error)
	Heartbeat(context.Context, string, *id.ID, time.Time) error
	WorkerStatus(context.Context, time.Time) (WorkerStatus, error)
	SelectImage(context.Context, id.ID, id.ID) error
}
type WorkerStatus struct {
	Connected          bool       `json:"connected"`
	WorkerID           string     `json:"workerId,omitempty"`
	LastSeenAt         *time.Time `json:"lastSeenAt,omitempty"`
	ActiveGenerationID *id.ID     `json:"activeGenerationId,omitempty"`
}

type PromptJob struct {
	ID, StoryID, TimelineID, SceneID, SourceBeatID id.ID
	ForceIllustration                              bool
	Attempts, MaxAttempts                          int
	AvailableAt                                    time.Time
}

type PromptJobRepository interface {
	ClaimPromptJob(context.Context, string, id.ID, time.Time, time.Duration) (PromptJob, error)
	CompletePromptJob(context.Context, id.ID, string, time.Time) error
	FailPromptJob(context.Context, id.ID, string, time.Time, time.Duration, string) error
}
