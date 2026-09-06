package generation

import (
	"context"
	"errors"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationattempts"
)

type callStore struct {
	calls []generationattempts.Call
	err   error
}

func (s *callStore) Record(_ context.Context, call generationattempts.Call) error {
	s.calls = append(s.calls, call)
	return s.err
}

type observedFake struct{ identity aiport.ProviderIdentity }

func (f observedFake) Identity() aiport.ProviderIdentity { return f.identity }
func (f observedFake) Generate(context.Context, aiport.StoryRequest) (aiport.StoryResponse, error) {
	return aiport.StoryResponse{Output: []byte(`{}`), InputTokens: 12, OutputTokens: 3, Provider: aiport.ProviderIdentity{Provider: "google_gemini", Model: "actual-model", Profile: "actual"}, EscalatedFrom: "fast-model"}, nil
}

func TestObservingLLMRecordsActualProviderAndCallSequence(t *testing.T) {
	store := &callStore{}
	llm := &observingLLM{base: observedFake{}, store: store, jobID: id.MustParse("00000000-0000-4000-8000-000000000801"), attemptNo: 2, contextMode: "lossless-dedupe-v1"}
	for _, role := range []string{"writer", "structured_repair"} {
		if _, err := llm.Generate(context.Background(), aiport.StoryRequest{Role: role, PromptVersion: "v1"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.calls) != 2 || store.calls[0].CallSequence != 1 || store.calls[1].CallSequence != 2 {
		t.Fatalf("bad call sequence: %#v", store.calls)
	}
	if store.calls[0].Provider.Model != "actual-model" || store.calls[0].EscalatedFrom != "fast-model" || store.calls[1].RepairCount != 1 {
		t.Fatalf("bad call provenance: %#v", store.calls)
	}
}

func TestObservingLLMTelemetryFailureDoesNotFailGeneration(t *testing.T) {
	store := &callStore{err: errors.New("telemetry unavailable")}
	llm := &observingLLM{base: observedFake{}, store: store, jobID: id.MustParse("00000000-0000-4000-8000-000000000802"), attemptNo: 1}
	if _, err := llm.Generate(context.Background(), aiport.StoryRequest{Role: "writer"}); err != nil {
		t.Fatalf("telemetry failure leaked into generation: %v", err)
	}
}
