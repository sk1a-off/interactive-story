package main

import (
	"encoding/json"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

// Explicitly synthetic fixture events, never persisted in the application.
func ledgerSuiteInput(f questFixture) (json.RawMessage, error) {
	var input struct {
		Context struct {
			Recent []struct {
				Text string `json:"text"`
			} `json:"recentBeats"`
		} `json:"context"`
	}
	if err := json.Unmarshal(f.Input, &input); err != nil {
		return nil, err
	}
	tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000701"))
	gid := event.GenerationID(id.MustParse("00000000-0000-4000-8000-000000000702"))
	beat, _ := json.Marshal(map[string]string{"text": input.Context.Recent[0].Text})
	payload, _ := json.Marshal(map[string]any{"objectiveId": "00000000-0000-4000-8000-000000000201", "progress": 33, "status": "active", "successCriteria": "Все три повозки надёжно закреплены канатами.", "evidence": input.Context.Recent[0].Text})
	source := []event.StoredEvent{
		{ID: event.ID(id.MustParse("00000000-0000-4000-8000-000000000703")), TimelineID: tid, Seq: 1, Type: "beat_committed", SchemaVersion: 1, GenerationID: &gid, Payload: beat},
		{ID: event.ID(id.MustParse("00000000-0000-4000-8000-000000000704")), TimelineID: tid, Seq: 2, Type: "objective_created", SchemaVersion: 1, GenerationID: &gid, Payload: payload},
	}
	return questInputWithLedger(f.Input, tid, 2, source)
}
