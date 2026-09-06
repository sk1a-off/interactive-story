package generation

import (
	"context"
	"errors"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type sequentialExtractorLLM struct {
	responses [][]byte
	requests  []aiport.StoryRequest
}

func (s *sequentialExtractorLLM) Identity() aiport.ProviderIdentity {
	return aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "test", Model: "sequential", Profile: "test"}
}

func (s *sequentialExtractorLLM) Generate(_ context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	s.requests = append(s.requests, request)
	index := len(s.requests) - 1
	if index >= len(s.responses) {
		return aiport.StoryResponse{}, aiport.ErrInvalidOutput
	}
	return aiport.StoryResponse{Output: append([]byte(nil), s.responses[index]...)}, nil
}

func TestCanonExtractorRepairsMissingEvidenceQuoteOnce(t *testing.T) {
	invalid := []byte(`{"worldChanges":[],"journalChanges":[{"operation":"create","category":"item","name":"Канат","quantity":1,"evidence":"поднял канат"}],"objectiveChanges":[]}`)
	valid := []byte(`{"worldChanges":[],"journalChanges":[{"operation":"create","category":"item","name":"Канат","quantity":1,"evidenceQuote":"поднял канат"}],"objectiveChanges":[]}`)
	llm := &sequentialExtractorLLM{responses: [][]byte{invalid, valid}}
	extractor := LLMCanonExtractor{LLM: llm, MaxRepairs: 1}
	target := generationtarget.Target{SceneID: id.MustParse("00000000-0000-4000-8000-000000000901")}

	result, err := extractor.Extract(context.Background(), PlayerAction{Text: "Поднять канат"}, target, "поднять канат", "Герой поднял канат.", direction{}, pacingDecision{})
	if err != nil {
		t.Fatal(err)
	}
	if len(llm.requests) != 2 || llm.requests[0].Repair || !llm.requests[1].Repair {
		t.Fatalf("unexpected repair requests: %+v", llm.requests)
	}
	if len(result.JournalChanges) != 1 || result.JournalChanges[0].Evidence != "поднял канат" {
		t.Fatalf("repair was not normalized: %+v", result)
	}
}

func TestCanonExtractorWithoutRepairRejectsMissingEvidenceQuote(t *testing.T) {
	invalid := []byte(`{"worldChanges":[],"journalChanges":[{"operation":"create","category":"item","name":"Канат","quantity":1,"evidence":"поднял канат"}],"objectiveChanges":[]}`)
	llm := &sequentialExtractorLLM{responses: [][]byte{invalid}}
	extractor := LLMCanonExtractor{LLM: llm}

	_, err := extractor.Extract(context.Background(), PlayerAction{Text: "Поднять канат"}, generationtarget.Target{}, "поднять канат", "Герой поднял канат.", direction{}, pacingDecision{})
	if !errors.Is(err, ErrRejectedProposal) {
		t.Fatalf("missing evidenceQuote accepted: %v", err)
	}
	if len(llm.requests) != 1 || llm.requests[0].Repair {
		t.Fatalf("unexpected calls without repair: %+v", llm.requests)
	}
}
