package generation

import (
	"context"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type questRepairLLM struct {
	base  aiport.StoryLLM
	quest *sequentialExtractorLLM
}

func (l questRepairLLM) Identity() aiport.ProviderIdentity { return l.base.Identity() }
func (l questRepairLLM) Generate(ctx context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	if request.Role == "quest_evaluator" {
		return l.quest.Generate(ctx, request)
	}
	return l.base.Generate(ctx, request)
}

func TestQuestRepairCommitsWithoutRepeatingWriter(t *testing.T) {
	base := scripted()
	base.Responses["writer"] = []byte(`{"text":"Алекс распахнул створку до самого упора. Дверь была открыта, и архив лежал перед Алексом."}`)
	objectiveID := "00000000-0000-4000-8000-000000000201"
	quest := &sequentialExtractorLLM{responses: [][]byte{
		[]byte(`{"changes":[{"operation":"complete","objectiveId":"` + objectiveID + `","evidenceQuote":"Алекс распахнул створку до самого упора."}]}`),
		[]byte(`{"changes":[{"operation":"complete","objectiveId":"` + objectiveID + `","evidenceQuote":"Дверь была открыта, и архив лежал перед Алексом."}]}`),
	}}
	pipeline := Pipeline{LLM: questRepairLLM{base: base, quest: quest}, Canon: &appender{}, Targets: staticTarget{target: generationtarget.Target{
		SceneID: id.MustParse("00000000-0000-4000-8000-000000000002"), NextBeatPosition: 2,
		Objectives: []generationtarget.Objective{{ID: objectiveID, Scope: "global", Kind: "quest", Title: "Открыть дверь", Status: "active", SuccessCriteria: "Дверь архива полностью открыта"}},
	}}}
	events, err := pipeline.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000003"), PlayerAction{TimelineID: timeline.ID("00000000-0000-4000-8000-000000000004"), Text: "Открыть дверь архива"})
	if err != nil {
		t.Fatal(err)
	}
	writers, objectives := 0, 0
	for _, role := range base.Calls {
		if role == "writer" {
			writers++
		}
	}
	for _, event := range events {
		if event.Type == "objective_updated" {
			objectives++
		}
	}
	if writers != 1 || objectives != 1 || len(quest.requests) != 2 || !quest.requests[1].Repair {
		t.Fatalf("writers=%d objectives=%d questCalls=%d", writers, objectives, len(quest.requests))
	}
}

func TestObjectiveEvidenceRepairIsBoundedAndRevalidated(t *testing.T) {
	current := []generationtarget.Objective{{ID: "goal", Status: "active", SuccessCriteria: "Дверь архива полностью открыта"}}
	beat := "Алекс распахнул створку до самого упора. Дверь была открыта, и архив лежал перед Алексом."
	rejected := []objectiveChange{{Operation: "complete", ObjectiveID: "goal", EvidenceQuote: "Алекс распахнул створку до самого упора."}}
	if _, err := prepareObjectiveEvidence(beat, current, rejected); err == nil {
		t.Fatal("fixture must reproduce lexical false negative")
	}
	for _, tc := range []struct {
		name, output string
		valid        bool
	}{
		{"direct alternate quote", `{"changes":[{"operation":"complete","objectiveId":"goal","evidenceQuote":"Дверь была открыта, и архив лежал перед Алексом."}]}`, true},
		{"historical quote", `{"changes":[{"operation":"complete","objectiveId":"goal","evidenceQuote":"Вчера дверь архива полностью открылась."}]}`, false},
		{"criterion rewrite", `{"changes":[{"operation":"complete","objectiveId":"goal","successCriteria":"Алекс распахнул створку","evidenceQuote":"Алекс распахнул створку до самого упора."}]}`, false},
		{"malformed", `{`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			llm := &sequentialExtractorLLM{responses: [][]byte{[]byte(tc.output)}}
			_, err := repairObjectiveEvidence(context.Background(), llm, beat, current, rejected)
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected error %v", err)
			}
			if len(llm.requests) != 1 || llm.requests[0].Role != "quest_evaluator" || !llm.requests[0].Repair {
				t.Fatal("repair budget or provenance violated")
			}
		})
	}
}
