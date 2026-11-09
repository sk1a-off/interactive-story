package main

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type questLedgerRecord struct {
	EventID       event.ID        `json:"eventId"`
	TimelineID    timeline.ID     `json:"timelineId"`
	Sequence      int64           `json:"sequence"`
	EventType     string          `json:"eventType"`
	RecordedState json.RawMessage `json:"recordedState"`
	Provenance    questProvenance `json:"provenance"`
}

type questLedgerContext struct {
	Version         string              `json:"version"`
	ThroughSequence int64               `json:"throughSequence"`
	HistoryRetained bool                `json:"historyRetained"`
	Records         []questLedgerRecord `json:"records"`
}

// Experimental only: add an auditable record of already committed quest changes.
// Source events must be supplied by a trusted snapshot reader, never by an LLM.
// A complete event prefix does NOT prove completeness of semantic conditions in
// prose. Therefore this version never deletes history, cast, rules or visuals.
// Any inconsistency returns the original input unchanged plus a diagnostic.
func questInputWithLedger(raw json.RawMessage, tid timeline.ID, head int64, source []event.StoredEvent) (json.RawMessage, error) {
	fail := func(reason string) (json.RawMessage, error) {
		return raw, fmt.Errorf("quest ledger fallback: %s", reason)
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(raw, &input) != nil || input == nil {
		return fail("invalid input")
	}
	if _, exists := input["questHistoryLedger"]; exists {
		return fail("ledger already supplied")
	}
	if tid == "" || head < 1 || int64(len(source)) != head {
		return fail("incomplete event prefix")
	}
	ledger := questLedgerContext{Version: "quest-ledger-shadow-v1", ThroughSequence: head, HistoryRetained: true, Records: []questLedgerRecord{}}
	latest := map[string]json.RawMessage{}
	seen := map[event.ID]bool{}
	for i, e := range source {
		if e.Validate() != nil || e.TimelineID != tid || e.Seq != int64(i+1) || seen[e.ID] {
			return fail("invalid, mixed or unordered event prefix")
		}
		seen[e.ID] = true
		switch e.Type {
		case "objective_created", "objective_updated":
			var p struct {
				ID       string `json:"objectiveId"`
				Progress *int   `json:"progress"`
				Status   string `json:"status"`
			}
			if json.Unmarshal(e.Payload, &p) != nil || p.ID == "" || p.Progress == nil || *p.Progress < 0 || *p.Progress > 100 || p.Status == "" {
				return fail("invalid objective event")
			}
			latest[p.ID] = e.Payload
			ledger.Records = append(ledger.Records, questLedgerRecord{EventID: e.ID, TimelineID: e.TimelineID, Sequence: e.Seq, EventType: e.Type, RecordedState: append(json.RawMessage(nil), e.Payload...), Provenance: classifyQuestProvenance(e, source[:i])})
		}
	}
	var objectives []map[string]json.RawMessage
	if json.Unmarshal(input["currentObjectives"], &objectives) != nil || objectives == nil {
		return fail("missing objectives")
	}
	for _, o := range objectives {
		var oid string
		if json.Unmarshal(o["id"], &oid) != nil || oid == "" {
			return fail("invalid objective id")
		}
		var last map[string]json.RawMessage
		if json.Unmarshal(latest[oid], &last) != nil {
			return fail("objective has no source event")
		}
		for _, field := range []string{"progress", "status", "successCriteria"} {
			var a, b any
			if json.Unmarshal(o[field], &a) != nil || json.Unmarshal(last[field], &b) != nil || !reflect.DeepEqual(a, b) {
				return fail("snapshot differs from event prefix")
			}
		}
	}
	data, err := json.Marshal(ledger)
	if err != nil {
		return raw, err
	}
	input["questHistoryLedger"] = data
	result, err := json.Marshal(input)
	if err != nil {
		return raw, err
	}
	return result, nil
}
