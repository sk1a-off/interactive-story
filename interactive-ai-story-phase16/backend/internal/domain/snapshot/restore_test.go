package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/projection"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type goldenFixture struct {
	EventSeq          int64           `json:"event_seq"`
	SchemaVersion     int             `json:"schema_version"`
	SerializerVersion int             `json:"serializer_version"`
	TimelineID        timeline.ID     `json:"timeline_id"`
	StateHash         string          `json:"state_hash"`
	State             json.RawMessage `json:"state"`
}

func loadGolden(t *testing.T) (Snapshot, []byte) {
	t.Helper()
	raw, err := os.ReadFile("testdata/legacy_serializer1_schema3.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture goldenFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	value := Snapshot{ID: ID(id.MustParse("33333333-3333-4333-8333-333333333333")), TimelineID: fixture.TimelineID, EventSeq: fixture.EventSeq, StateHash: fixture.StateHash, SchemaVersion: fixture.SchemaVersion, SerializerVersion: fixture.SerializerVersion, CreatedAt: time.Unix(5, 0).UTC()}
	if err := json.Unmarshal(fixture.State, &value.State); err != nil {
		t.Fatal(err)
	}
	return value, fixture.State
}

// The golden file was written by the legacy serializer against the state type
// as it existed at that time. It must keep restoring (and forking) no matter
// how the state type evolves afterwards.
func TestRestoreAcceptsGoldenLegacySnapshotAndUpgradesIt(t *testing.T) {
	value, stored := loadGolden(t)
	restored, err := Restore(value, stored)
	if err != nil {
		t.Fatalf("golden legacy snapshot rejected: %v", err)
	}
	if restored.SerializerVersion != SerializerCanonical || restored.SchemaVersion != projection.SchemaVersion || restored.State.SchemaVersion != projection.SchemaVersion {
		t.Fatalf("legacy snapshot was not upgraded in memory: %+v", restored)
	}
	if restored.EventSeq != 7 || len(restored.State.Narrative.Chapters) != 1 || len(restored.State.Narrative.WorldResources) != 1 {
		t.Fatalf("restored content lost: %+v", restored.State.Narrative)
	}
	if err := restored.Verify(); err != nil {
		t.Fatalf("upgraded snapshot does not verify: %v", err)
	}
	child, err := CloneForTimelineWithIDs(ID(id.MustParse("44444444-4444-4444-8444-444444444444")), restored, timeline.ID(id.MustParse("55555555-5555-4555-8555-555555555555")), time.Unix(6, 0).UTC(), id.New)
	if err != nil {
		t.Fatalf("fork of restored legacy snapshot failed: %v", err)
	}
	if child.EventSeq != 7 || len(child.State.Narrative.Chapters) != 1 {
		t.Fatalf("fork lost state: %+v", child)
	}
}

// Simulates a snapshot written before the state type gained a field: the
// stored document lacks the member, the legacy hash covers the old bytes, and
// re-encoding with the current type adds the member as a zero value.
func TestRestoreToleratesLegacySnapshotWrittenBeforeFieldWasAdded(t *testing.T) {
	value, stored := loadGolden(t)
	var doc map[string]any
	if err := json.Unmarshal(stored, &doc); err != nil {
		t.Fatal(err)
	}
	narrative := doc["narrative"].(map[string]any)
	delete(narrative, "DirectorInstructions")
	delete(narrative, "WorldRules")
	olderStored, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var decoded projection.TimelineState
	if err := json.Unmarshal(olderStored, &decoded); err != nil {
		t.Fatal(err)
	}
	value.State = decoded
	value.StateHash = sha256Hex(olderStored)
	if err := value.Verify(); err == nil {
		t.Fatal("exact legacy verification should fail once the type re-encodes new members")
	}
	restored, err := Restore(value, olderStored)
	if err != nil {
		t.Fatalf("additive schema evolution must be tolerated: %v", err)
	}
	if restored.SerializerVersion != SerializerCanonical || restored.SchemaVersion != projection.SchemaVersion {
		t.Fatalf("restored snapshot not normalized: %+v", restored)
	}
}

// A stored member the current type no longer decodes would silently vanish on
// the next save; the lossless check must refuse such a snapshot.
func TestRestoreRejectsLegacySnapshotWhoseDataWouldBeDropped(t *testing.T) {
	value, stored := loadGolden(t)
	var doc map[string]any
	if err := json.Unmarshal(stored, &doc); err != nil {
		t.Fatal(err)
	}
	doc["narrative"].(map[string]any)["RetiredCollection"] = map[string]any{"x": 1}
	alteredStored, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	value.StateHash = sha256Hex(alteredStored)
	if _, err := Restore(value, alteredStored); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("expected ErrInvalidSnapshot, got %v", err)
	}
}

func TestRestoreUpgradesOlderSchemaVersion(t *testing.T) {
	value, stored := loadGolden(t)
	var doc map[string]any
	if err := json.Unmarshal(stored, &doc); err != nil {
		t.Fatal(err)
	}
	doc["schema_version"] = 2
	olderStored, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(olderStored, &value.State); err != nil {
		t.Fatal(err)
	}
	value.SchemaVersion = 2
	value.StateHash = sha256Hex(olderStored)
	restored, err := Restore(value, olderStored)
	if err != nil {
		t.Fatalf("schema v2 snapshot rejected: %v", err)
	}
	if restored.SchemaVersion != projection.SchemaVersion || restored.State.SchemaVersion != projection.SchemaVersion {
		t.Fatalf("schema version not upgraded: %+v", restored)
	}
	value.SchemaVersion = projection.SchemaVersion + 1
	value.State.SchemaVersion = projection.SchemaVersion + 1
	if _, err := Restore(value, olderStored); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("future schema version must be rejected, got %v", err)
	}
}

func TestRestoreRejectsUnknownSerializerAndCanonicalTampering(t *testing.T) {
	tidRaw, _ := id.New()
	sidRaw, _ := id.New()
	snap, err := New(ID(sidRaw), projection.Empty(timeline.ID(tidRaw)), time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if snap.SerializerVersion != SerializerCanonical {
		t.Fatalf("new snapshots must use the canonical serializer, got %d", snap.SerializerVersion)
	}
	stored, err := projection.CanonicalJSON(snap.State)
	if err != nil {
		t.Fatal(err)
	}
	tampered := snap
	tampered.State.AppliedEventCount++
	if _, err := Restore(tampered, stored); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("tampered canonical snapshot accepted: %v", err)
	}
	unknown := snap
	unknown.SerializerVersion = 99
	if _, err := Restore(unknown, stored); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("unknown serializer accepted: %v", err)
	}
}

func TestCanonicalSnapshotSurvivesJSONBStyleRoundTrip(t *testing.T) {
	value, stored := loadGolden(t)
	restored, err := Restore(value, stored)
	if err != nil {
		t.Fatal(err)
	}
	// jsonb reorders keys and respells whitespace; decode/encode through a
	// generic map to imitate that before re-decoding into the state type.
	raw, err := projection.CanonicalJSON(restored.State)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	respelled, err := json.MarshalIndent(generic, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	reloaded := restored
	reloaded.State = projection.TimelineState{}
	if err := json.Unmarshal(respelled, &reloaded.State); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(reloaded, respelled); err != nil {
		t.Fatalf("canonical snapshot rejected after jsonb-style round trip: %v", err)
	}
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
