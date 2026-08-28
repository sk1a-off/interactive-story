package projection

import (
	"encoding/json"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

func TestReplayRequiresContiguousTimelineEvents(t *testing.T) {
	tidRaw, _ := id.New()
	tid := timeline.ID(tidRaw)
	eid1, _ := id.New()
	eid2, _ := id.New()
	events := []event.StoredEvent{
		{ID: event.ID(eid1), TimelineID: tid, Seq: 1, Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{}`)},
		{ID: event.ID(eid2), TimelineID: tid, Seq: 2, Type: "choices_ready", SchemaVersion: 1, Payload: json.RawMessage(`{"choices":[]}`)},
	}
	state, err := Replay(tid, events)
	if err != nil {
		t.Fatal(err)
	}
	if state.LastEventSeq != 2 || state.AppliedEventCount != 2 {
		t.Fatalf("unexpected state: %+v", state)
	}

	events[1].Seq = 3
	if _, err := Replay(tid, events); err == nil {
		t.Fatal("expected non-contiguous replay failure")
	}
}
