package repositories

import (
	"context"

	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/savepoint"
	"github.com/local/interactive-ai-story/backend/internal/domain/snapshot"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type StoryRepository interface {
	Create(context.Context, story.Story) error
	Get(context.Context, story.ID) (story.Story, error)
}

type TimelineRepository interface {
	Create(context.Context, timeline.Timeline) error
	Get(context.Context, timeline.ID) (timeline.Timeline, error)
	Lock(context.Context, timeline.ID) (timeline.Timeline, error)
	ListByStory(context.Context, story.ID) ([]timeline.Timeline, error)
	AdvanceHead(context.Context, timeline.ID, int64) error
	SetHeadSnapshot(context.Context, timeline.ID, snapshot.ID) error
}

type EventRepository interface {
	Append(context.Context, timeline.ID, []event.StoredEvent) error
	List(context.Context, timeline.ID, int64) ([]event.StoredEvent, error)
}

type SnapshotRepository interface {
	Create(context.Context, snapshot.Snapshot) error
	Get(context.Context, snapshot.ID) (snapshot.Snapshot, error)
	Latest(context.Context, timeline.ID) (snapshot.Snapshot, error)
}

type NarrativeProjectionRepository interface {
	Export(context.Context, timeline.ID) (narrative.State, error)
	Materialize(context.Context, narrative.State) error
}

type SavePointRepository interface {
	Create(context.Context, savepoint.SavePoint) error
	Get(context.Context, savepoint.ID) (savepoint.SavePoint, error)
	ListByTimeline(context.Context, timeline.ID) ([]savepoint.SavePoint, error)
	UpdatePresentation(context.Context, savepoint.ID, *int, bool) error
}

type CanonRepositories interface {
	Stories() StoryRepository
	Timelines() TimelineRepository
	Events() EventRepository
	Snapshots() SnapshotRepository
	SavePoints() SavePointRepository
	Narrative() NarrativeProjectionRepository
}
