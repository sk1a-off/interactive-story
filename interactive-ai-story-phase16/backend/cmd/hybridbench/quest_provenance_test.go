package main

import (
	"encoding/json"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

func provenanceEvent(seq int64, typ, payload string) event.StoredEvent {
	i, _ := id.New()
	g := event.GenerationID(id.MustParse("00000000-0000-4000-8000-000000000600"))
	return event.StoredEvent{ID: event.ID(i), TimelineID: ledgerTestTimeline, Seq: seq, Type: typ, SchemaVersion: 1, GenerationID: &g, Payload: json.RawMessage(payload)}
}
func TestQuestProvenanceQuoteAndLegacy(t *testing.T) {
	beat := provenanceEvent(1, "beat_committed", `{"text":"Первая повозка надёжно закреплена."}`)
	update := provenanceEvent(2, "objective_updated", `{"objectiveId":"q","evidence":"Первая  повозка надёжно закреплена."}`)
	if got := classifyQuestProvenance(update, []event.StoredEvent{beat}); got.Kind != "direct_quote" || !got.VerifiedQuote || got.SourceEventIDs[0] != beat.ID {
		t.Fatal(got)
	}
	beat.GenerationID = nil
	if got := classifyQuestProvenance(update, []event.StoredEvent{beat}); got.Kind != "unverified_legacy" {
		t.Fatal(got)
	}
	update.GenerationID = nil
	if got := classifyQuestProvenance(update, nil); got.Kind != "initial_or_manual" {
		t.Fatal(got)
	}
}
func TestQuestProvenanceRequiresCorrectAggregation(t *testing.T) {
	parent := provenanceEvent(1, "objective_created", `{"objectiveId":"q","scope":"global","status":"active","progress":0}`)
	child := provenanceEvent(2, "objective_updated", `{"objectiveId":"c","parentObjectiveId":"q","scope":"minor","status":"active","progress":50}`)
	update := provenanceEvent(3, "objective_updated", `{"objectiveId":"q","scope":"global","status":"active","progress":50,"evidence":""}`)
	before := []event.StoredEvent{parent, child}
	got := classifyQuestProvenance(update, before)
	if got.Kind != "derived_progress_consistent" || got.VerifiedQuote || len(got.SourceEventIDs) != 2 {
		t.Fatal(got)
	}
	update.Payload = json.RawMessage(`{"objectiveId":"q","scope":"global","status":"active","progress":60}`)
	if got = classifyQuestProvenance(update, before); got.Kind != "unverified_legacy" {
		t.Fatal(got)
	}
}
