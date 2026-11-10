package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestPairedCorpusScheduleAndGroundTruth(t *testing.T) {
	cases := pairedCorpus()
	writers, structured, pairs := 0, 0, 0
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.ID] || !json.Valid(c.Input) {
			t.Fatal("invalid corpus", c.ID)
		}
		seen[c.ID] = true
		pairs += c.Repeats
		if c.Writer {
			writers++
			if len(c.Facts) < 3 {
				t.Fatal("missing oracle facts")
			}
		} else {
			structured++
		}
	}
	if writers != 8 || structured != 12 || pairs != 48 {
		t.Fatal(writers, structured, pairs)
	}
	m := pairedModels{prompts: promptset.Revision{Prompts: map[string]string{"writer": "frozen"}}}
	a := newPairedPlan(pairedOptions{Seed: 42}, m, cases)
	b := newPairedPlan(pairedOptions{Seed: 42}, m, cases)
	if !reflect.DeepEqual(a.Schedule, b.Schedule) {
		t.Fatal("schedule not reproducible")
	}
	counts := map[string]int{}
	for _, s := range a.Schedule {
		counts[s.FirstArm]++
	}
	if counts["chatgpt"] != 24 || counts["antigravity"] != 24 {
		t.Fatal("unbalanced arm assignment", counts)
	}
	for _, c := range cases {
		if c.ID == "journal_planned_purchase" || c.ID == "journal_metaphor" || c.ID == "world_absence" || c.ID == "quest_planned_not_complete" {
			v := c.validate(map[string]any{"changes": []any{}}, `{"changes":[]}`)
			if v.SchemaScore != 1 || v.InstructionScore != 1 {
				t.Fatal("negative oracle rejected", c.ID, v)
			}
		}
	}
}

type pairTestModel struct {
	arm   string
	calls int
}

func (m *pairTestModel) Identity() ai.ProviderIdentity { return ai.ProviderIdentity{Model: m.arm} }
func (m *pairTestModel) Generate(_ context.Context, _ ai.StoryRequest) (ai.StoryResponse, error) {
	m.calls++
	return ai.StoryResponse{Output: []byte(`{"text":"Ответ"}`)}, nil
}
func TestPairCheckpointsPreventResendingAndInputsMatch(t *testing.T) {
	root := t.TempDir()
	a, b := &pairTestModel{arm: "a"}, &pairTestModel{arm: "b"}
	m := pairedModels{models: map[string]ai.StoryLLM{"chatgpt": a, "antigravity": b}}
	c := pairedCase{ID: "one", Role: "writer", Input: json.RawMessage(`{"intent":"same"}`)}
	s := pairSpec{ID: "one-r1", CaseID: "one", FirstArm: "chatgpt"}
	r := runPair(context.Background(), root, m, c, s)
	if r.Outcomes["chatgpt"].InputSHA256 != r.Outcomes["antigravity"].InputSHA256 {
		t.Fatal("mismatched inputs")
	}
	runPair(context.Background(), root, m, c, s)
	if a.calls != 1 || b.calls != 1 {
		t.Fatal("completed job retried")
	}
	_ = os.Remove(filepath.Join(root, "outcomes", s.ID+"-chatgpt.json"))
	r = runPair(context.Background(), root, m, c, s)
	if a.calls != 1 || r.Outcomes["chatgpt"].ErrorCode != "interrupted_not_retried" {
		t.Fatal("uncertain job retried")
	}
}
func TestJudgeRejectsInventedEvidenceAndOutOfRangeScores(t *testing.T) {
	score := map[string]any{"continuity": 4, "agency": 4, "sceneProgress": 4, "prose": 4, "nonRepetition": 4}
	v := map[string]any{"A": score, "B": score, "winner": "tie", "criticalErrorsA": []any{}, "criticalErrorsB": []any{}}
	raw, _ := json.Marshal(v)
	if !validJudgeVerdict(raw, "text a", "text b") {
		t.Fatal("valid rating rejected")
	}
	v["criticalErrorsA"] = []any{map[string]string{"type": "agency", "quote": "invented"}}
	raw, _ = json.Marshal(v)
	if validJudgeVerdict(raw, "text a", "text b") {
		t.Fatal("invented quote accepted")
	}
}
