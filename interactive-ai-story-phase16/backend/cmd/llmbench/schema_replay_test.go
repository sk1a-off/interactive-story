package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
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
func TestSchemaFrozenOutputsReachCanonValidation(t *testing.T) {
	probes, err := schemaProbes()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range probes {
		if p.Expected != "negative" {
			continue
		}
		row := replaySchemaEvaluator(p, schemaBenchRow{Case: p.ID, Output: `{"changes":[]}`})
		if !row.Passed {
			t.Fatalf("negative replay failed: %+v", row)
		}
	}
}
func TestSchemaLiveReportThroughProductionValidators(t *testing.T) {
	path := os.Getenv("SCHEMA_PROBE_REPORT")
	if path == "" {
		t.Skip("set SCHEMA_PROBE_REPORT to a completed synthetic role benchmark")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var source schemaBenchReport
	if json.Unmarshal(raw, &source) != nil || source.Status != "measured" {
		t.Fatal("completed measured report required")
	}
	probes, err := schemaProbes()
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]schemaProbe{}
	for _, p := range probes {
		by[p.ID] = p
	}
	type replayReport struct {
		SourceSHA256 string            `json:"sourceSha256"`
		Rows         []schemaReplayRow `json:"rows"`
		Notes        []string          `json:"notes"`
	}
	hash := sha256.Sum256(raw)
	result := replayReport{SourceSHA256: hex.EncodeToString(hash[:]), Notes: []string{"Offline replay of live evaluator outputs through production Pipeline validation and in-memory Canon capture; no new provider requests or SQL writes.", "The other roles are scripted. Frozen positive/negative mutation counts do not validate all semantic content, branch leakage, or literary quality."}}
	for _, row := range source.Runs {
		p, ok := by[row.Case]
		if !ok {
			t.Fatal("unknown frozen case")
		}
		if p.Expected == "" {
			continue
		}
		h := sha256.Sum256(p.Request.Input)
		if hex.EncodeToString(h[:]) != row.InputSHA256 {
			t.Fatal("source input hash differs from replay fixture")
		}
		result.Rows = append(result.Rows, replaySchemaEvaluator(p, row))
	}
	if len(result.Rows) != 24 {
		t.Fatalf("incomplete evaluator coverage: %d", len(result.Rows))
	}
	if output := os.Getenv("SCHEMA_REPLAY_OUTPUT"); output != "" {
		inputInfo, _ := os.Stat(path)
		outputInfo, _ := os.Stat(output)
		if outputInfo != nil && os.SameFile(inputInfo, outputInfo) {
			t.Fatal("replay output aliases source")
		}
		body, _ := json.MarshalIndent(result, "", "  ")
		if err := os.WriteFile(output, append(body, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range result.Rows {
		if !row.Passed {
			t.Errorf("%s repeat=%d schema=%t: %s mutations=%d", row.Case, row.Repeat, row.Schema, row.Failure, row.Mutations)
		}
	}
}
