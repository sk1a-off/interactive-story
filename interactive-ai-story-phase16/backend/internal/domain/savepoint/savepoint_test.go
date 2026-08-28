package savepoint

import (
	"errors"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/projection"
	"github.com/local/interactive-ai-story/backend/internal/domain/snapshot"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

func sid(v string) id.ID { return id.MustParse("00000000-0000-4000-8000-" + v) }

func TestSavePointRequiresSnapshotFromSameTimeline(t *testing.T) {
	source := timeline.ID(sid("000000000001"))
	other := timeline.ID(sid("000000000002"))
	state := projection.Empty(other)
	snap, err := snapshot.New(snapshot.ID(sid("000000000003")), state, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}

	_, err = New(ID(sid("000000000004")), story.ID(sid("000000000005")), source, snap, "save", "", KindManual, false, time.Unix(2, 0).UTC())
	if !errors.Is(err, ErrInvalidSavePoint) {
		t.Fatalf("expected timeline mismatch rejection, got %v", err)
	}
}
