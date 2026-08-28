package savepoint

import (
	"errors"
	"strings"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/snapshot"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type ID id.ID

type Kind string

const (
	KindManual          Kind = "manual"
	KindAutosave        Kind = "autosave"
	KindPreRestore      Kind = "pre_restore"
	KindPreRegeneration Kind = "pre_regeneration"
	KindSystem          Kind = "system"
)

var ErrInvalidSavePoint = errors.New("invalid save point")

type SavePoint struct {
	ID               ID
	StoryID          story.ID
	TimelineID       timeline.ID
	SnapshotID       snapshot.ID
	EventSeq         int64
	Name             string
	Note             string
	Kind             Kind
	Pinned           bool
	DisplaySlot      *int
	ThumbnailStatus  string
	ThumbnailAssetID *id.ID
	ChapterID        *id.ID
	SceneID          *id.ID
	BeatID           *id.ID
	CreatedAt        time.Time
}

func New(saveID ID, storyID story.ID, timelineID timeline.ID, snap snapshot.Snapshot, name, note string, kind Kind, pinned bool, now time.Time) (SavePoint, error) {
	name = strings.TrimSpace(name)
	note = strings.TrimSpace(note)
	if id.ID(saveID).IsZero() || id.ID(storyID).IsZero() || id.ID(timelineID).IsZero() || name == "" || now.IsZero() {
		return SavePoint{}, ErrInvalidSavePoint
	}
	if snap.TimelineID != timelineID || snap.EventSeq < 0 || id.ID(snap.ID).IsZero() {
		return SavePoint{}, ErrInvalidSavePoint
	}
	if err := snap.Verify(); err != nil {
		return SavePoint{}, ErrInvalidSavePoint
	}
	switch kind {
	case KindManual, KindAutosave, KindPreRestore, KindPreRegeneration, KindSystem:
	default:
		return SavePoint{}, ErrInvalidSavePoint
	}
	return SavePoint{
		ID: saveID, StoryID: storyID, TimelineID: timelineID, SnapshotID: snap.ID,
		EventSeq: snap.EventSeq, Name: name, Note: note, Kind: kind, Pinned: pinned, CreatedAt: now,
	}, nil
}

func (s SavePoint) Validate() error {
	if id.ID(s.ID).IsZero() || id.ID(s.StoryID).IsZero() || id.ID(s.TimelineID).IsZero() || id.ID(s.SnapshotID).IsZero() || s.EventSeq < 0 || strings.TrimSpace(s.Name) == "" || s.CreatedAt.IsZero() {
		return ErrInvalidSavePoint
	}
	switch s.Kind {
	case KindManual, KindAutosave, KindPreRestore, KindPreRegeneration, KindSystem:
	default:
		return ErrInvalidSavePoint
	}
	return nil
}
