package projection

import (
	"errors"
	"strings"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

func TestReplayRejectsNewerEventSchemaVersionsWithContext(t *testing.T) {
	tid := timeline.ID(id.MustParse("66666666-6666-4666-8666-666666666666"))
	newer := event.StoredEvent{ID: event.ID(id.MustParse("77777777-7777-4777-8777-777777777777")), TimelineID: tid, Seq: 1, Type: "player_action_attempted", SchemaVersion: MaxEventSchemaVersion + 1, Payload: []byte(`{}`)}
	_, err := Replay(tid, []event.StoredEvent{newer})
	if !errors.Is(err, ErrUnsupportedEventVersion) || !strings.Contains(err.Error(), "player_action_attempted") || !strings.Contains(err.Error(), "seq=1") {
		t.Fatalf("newer schema version must fail replay with context, got %v", err)
	}
	unknown := newer
	unknown.SchemaVersion = 1
	unknown.Type = "mystery_event"
	_, err = Replay(tid, []event.StoredEvent{unknown})
	if !errors.Is(err, ErrUnsupportedSemanticEvent) || !strings.Contains(err.Error(), "mystery_event") {
		t.Fatalf("unknown type must fail replay with context, got %v", err)
	}
	if event.CurrentSchemaVersion != MaxEventSchemaVersion {
		t.Fatal("writers and reducers must agree on the current schema version")
	}
}

func TestCanonicalizeJSONSortsKeysAndElidesZeroMembers(t *testing.T) {
	raw := []byte(`{"z":1,"a":{"empty":"","n":0,"f":false,"list":[],"obj":{},"nested":{"keep":"x","drop":null}},"arr":[0,"",null,{"k":0}],"num":1.50,"exp":1e-07}`)
	got, err := CanonicalizeJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":{"nested":{"keep":"x"}},"arr":[0,"",null,{}],"exp":1e-07,"num":1.5,"z":1}`
	if string(got) != want {
		t.Fatalf("canonical form mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestCanonicalHashIgnoresAddedZeroFieldsButNotDroppedData(t *testing.T) {
	base := []byte(`{"schema_version":3,"timeline_id":"t1","narrative":{"Chapters":{"c1":{"Title":"One"}}}}`)
	withZeroField := []byte(`{"schema_version":3,"timeline_id":"t1","narrative":{"Chapters":{"c1":{"Title":"One","Summary":""}},"NewMap":null}}`)
	withData := []byte(`{"schema_version":3,"timeline_id":"t1","narrative":{"Chapters":{"c1":{"Title":"One","Summary":"lost"}}}}`)
	jsonbSpelling := []byte(`{"narrative": {"Chapters": {"c1": {"Title": "One"}}}, "timeline_id": "t1", "schema_version": 3}`)
	h := func(raw []byte) string {
		v, err := CanonicalHashJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if h(base) != h(withZeroField) {
		t.Fatal("zero-valued added field must not change the canonical hash")
	}
	if h(base) != h(jsonbSpelling) {
		t.Fatal("key order and whitespace must not change the canonical hash")
	}
	if h(base) == h(withData) {
		t.Fatal("a non-zero member must change the canonical hash")
	}
}

func TestCanonicalNumberNormalizesSpelling(t *testing.T) {
	cases := map[string]string{"0.0000001": "1e-07", "1.0": "1", "42": "42", "-0": "0", "0.5": "0.5", "1e+21": "1e+21"}
	for in, want := range cases {
		got, err := CanonicalizeJSON([]byte(`{"n":` + in + `}`))
		if err != nil {
			t.Fatal(err)
		}
		if want == "0" {
			if string(got) != `{}` {
				t.Fatalf("%s: zero should be elided, got %s", in, got)
			}
			continue
		}
		if string(got) != `{"n":`+want+`}` {
			t.Fatalf("%s: got %s want %s", in, got, want)
		}
	}
}

func TestUpgradeRejectsUnknownVersionsAndStampsCurrent(t *testing.T) {
	state := TimelineState{SchemaVersion: 1, TimelineID: "t1"}
	upgraded, err := Upgrade(state)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.SchemaVersion != SchemaVersion || upgraded.Narrative.WorldSystems == nil {
		t.Fatalf("upgrade did not normalize state: %+v", upgraded)
	}
	if _, err := Upgrade(TimelineState{SchemaVersion: SchemaVersion + 1}); err == nil {
		t.Fatal("future schema version must be rejected")
	}
	if _, err := Upgrade(TimelineState{SchemaVersion: 0}); err == nil {
		t.Fatal("zero schema version must be rejected")
	}
}
