package snapshot

import (
	"bytes"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/projection"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

func TestSnapshotDetectsStateTampering(t *testing.T) {
	tidRaw, _ := id.New()
	sidRaw, _ := id.New()
	state := projection.Empty(timeline.ID(tidRaw))
	snap, err := New(ID(sidRaw), state, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := snap.Verify(); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
	snap.State.AppliedEventCount++
	if err := snap.Verify(); err == nil {
		t.Fatal("tampered snapshot accepted")
	}
}

func TestDirectorInstructionKeepsLegacySnapshotFieldNames(t *testing.T) {
	timelineID := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000081"))
	instructionID := id.MustParse("00000000-0000-4000-8000-000000000082")
	state := projection.Empty(timelineID)
	state.Narrative.DirectorInstructions[instructionID] = narrative.DirectorInstruction{
		ID: instructionID, TimelineID: id.ID(timelineID), Text: "Keep moving", Scope: narrative.ScopePersistent,
		Priority: narrative.PriorityHigh, Status: "active", CreatedAtSeq: 4,
	}
	raw, err := projection.CanonicalJSON(state)
	if err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`"ID":"00000000-0000-4000-8000-000000000082","TimelineID":"00000000-0000-4000-8000-000000000081","Text":"Keep moving"`)
	if !bytes.Contains(raw, legacy) {
		t.Fatalf("snapshot serialization contract changed: %s", raw)
	}
}

func TestCloneForTimelinePreservesSequenceButRebindsIdentity(t *testing.T) {
	sourceID := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000071"))
	childID := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000072"))
	source, err := New(ID(id.MustParse("00000000-0000-4000-8000-000000000073")), projection.TimelineState{SchemaVersion: projection.SchemaVersion, TimelineID: sourceID, LastEventSeq: 42, AppliedEventCount: 42}, time.Unix(10, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	child, err := CloneForTimeline(ID(id.MustParse("00000000-0000-4000-8000-000000000074")), source, childID, time.Unix(11, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if child.TimelineID != childID || child.EventSeq != 42 || child.State.AppliedEventCount != 42 {
		t.Fatalf("unexpected child snapshot: %+v", child)
	}
	if child.StateHash == source.StateHash {
		t.Fatal("timeline identity must participate in snapshot hash")
	}
}
