package main

import (
	"context"
	"encoding/json"
	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"strings"
)

type schemaReplayCanon struct {
	turnCaptureCanon
	types []string
}

func (c *schemaReplayCanon) AppendSemantic(ctx context.Context, tid timeline.ID, head int64, events []event.PendingEvent) ([]event.StoredEvent, error) {
	stored, err := c.turnCaptureCanon.AppendSemantic(ctx, tid, head, events)
	if err == nil {
		for _, e := range events {
			c.types = append(c.types, e.Type)
		}
	}
	return stored, err
}

type schemaReplayRow struct {
	Case      string `json:"case"`
	Repeat    int    `json:"repeat"`
	Schema    bool   `json:"schema"`
	Passed    bool   `json:"passed"`
	Mutations int    `json:"mutations"`
	Failure   string `json:"failure,omitempty"`
}

func replaySchemaEvaluator(p schemaProbe, row schemaBenchRow) schemaReplayRow {
	result := schemaReplayRow{Case: row.Case, Repeat: row.Repeat, Schema: row.Schema}
	var input struct{ Action, NewBeat string }
	if json.Unmarshal(p.Request.Input, &input) != nil {
		result.Failure = "invalid_frozen_input"
		return result
	}
	f := turnFixtures()[0]
	f.Action, f.FakeText = input.Action, input.NewBeat
	model := fakeTurnModel(f).(*fakeai.ScriptedStoryLLM)
	model.Responses[p.Request.Role] = []byte(row.Output)
	canon := &schemaReplayCanon{}
	pipeline := appgen.Pipeline{LLM: model, Planner: appgen.LLMTurnPlanner{LLM: model}, Canon: canon, Targets: preparedTurnFixture{Fixture: f}, JournalEvidenceIsolationV1: true}
	gid, _ := id.New()
	tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000008010"))
	_, err := pipeline.Run(context.Background(), gid, appgen.PlayerAction{TimelineID: tid, Text: f.Action})
	if err != nil {
		result.Failure = "pipeline_rejected_output"
		return result
	}
	for _, kind := range canon.types {
		switch p.Request.Role {
		case "world_evaluator":
			if kind == "character_world_upserted" || kind == "location_world_upserted" {
				result.Mutations++
			}
		case "state_evaluator":
			if strings.HasPrefix(kind, "journal_entry_") {
				result.Mutations++
			}
		case "quest_evaluator":
			if strings.HasPrefix(kind, "objective_") {
				result.Mutations++
			}
		}
	}
	result.Passed = (p.Expected == "positive" && result.Mutations > 0) || (p.Expected == "negative" && result.Mutations == 0)
	if !result.Passed {
		result.Failure = "mutation_count_mismatched_frozen_label"
	}
	return result
}
