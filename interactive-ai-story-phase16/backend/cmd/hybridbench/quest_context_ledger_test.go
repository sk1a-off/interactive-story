package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

var ledgerTestTimeline = timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000501"))

// Hand-authored source events belong only to these synthetic fixtures. No
// production event references are fabricated or inferred from natural language.
func ledgerFixtureSource(f questFixture) []event.StoredEvent {
	var input struct {
		Context struct {
			Recent []struct {
				Text string `json:"text"`
			} `json:"recentBeats"`
		} `json:"context"`
	}
	_ = json.Unmarshal(f.Input, &input)
	payload, _ := json.Marshal(map[string]any{"objectiveId": "00000000-0000-4000-8000-000000000201", "progress": 33, "status": "active", "successCriteria": "Все три повозки надёжно закреплены канатами.", "evidence": input.Context.Recent[0].Text})
	return []event.StoredEvent{{ID: event.ID(id.MustParse("00000000-0000-4000-8000-000000000502")), TimelineID: ledgerTestTimeline, Seq: 1, Type: "objective_created", SchemaVersion: 1, Payload: payload}}
}

func TestQuestLedgerPreservesHistoryAndDistinguishingEvidence(t *testing.T) {
	fixtures := historyIdentityFixtures()
	var ledgers []json.RawMessage
	for _, f := range fixtures {
		source := ledgerFixtureSource(f)
		raw, err := questInputWithLedger(f.Input, ledgerTestTimeline, 1, source)
		if err != nil {
			t.Fatal(err)
		}
		var original, result map[string]json.RawMessage
		json.Unmarshal(f.Input, &original)
		json.Unmarshal(raw, &result)
		ledgers = append(ledgers, result["questHistoryLedger"])
		var ledger questLedgerContext
		json.Unmarshal(result["questHistoryLedger"], &ledger)
		if !ledger.HistoryRetained || len(ledger.Records) != 1 || ledger.Records[0].Sequence != 1 || ledger.Records[0].EventID != source[0].ID || !bytes.Equal(ledger.Records[0].RecordedState, source[0].Payload) {
			t.Fatal("source provenance lost")
		}
		delete(result, "questHistoryLedger")
		if !reflect.DeepEqual(original, result) {
			t.Fatal("original context changed")
		}
	}
	if bytes.Equal(ledgers[0], ledgers[1]) {
		t.Fatal("already-counted distinction lost")
	}
}

func TestQuestLedgerFallsBackOnUntrustedOrStaleSource(t *testing.T) {
	f := historyIdentityFixtures()[0]
	for _, tc := range []string{"missing", "gap", "other-timeline", "stale-head", "wrong-progress", "malformed-payload", "duplicate-id", "reordered"} {
		t.Run(tc, func(t *testing.T) {
			source := ledgerFixtureSource(f)
			head := int64(1)
			switch tc {
			case "missing":
				source = nil
			case "gap":
				source[0].Seq = 2
			case "other-timeline":
				source[0].TimelineID = timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000599"))
			case "stale-head":
				head = 2
			case "wrong-progress":
				source[0].Payload = bytes.Replace(source[0].Payload, []byte(`"progress":33`), []byte(`"progress":66`), 1)
			case "malformed-payload":
				source[0].Payload = json.RawMessage(`{"objectiveId":"x"}`)
			case "duplicate-id":
				source = append(source, source[0])
				source[1].Seq = 2
				head = 2
			case "reordered":
				source[0].Seq = 0
			}
			raw, err := questInputWithLedger(f.Input, ledgerTestTimeline, head, source)
			if err == nil || !bytes.Equal(raw, f.Input) {
				t.Fatal("unsafe fallback")
			}
		})
	}
}
