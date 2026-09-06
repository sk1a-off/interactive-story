package generation

import (
	"context"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	"github.com/local/interactive-ai-story/backend/internal/application/contextbuilder"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationmetrics"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

type shadowMemory struct {
	query memory.Query
	rows  []memory.Memory
}

func (s *shadowMemory) Search(_ context.Context, query memory.Query) ([]memory.Memory, error) {
	s.query = query
	return s.rows, nil
}

func TestContextShadowBuildsScopedContextWithoutChangingWriterInput(t *testing.T) {
	storyID := id.MustParse("00000000-0000-4000-8000-000000000830")
	timelineID := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000831"))
	memoryStore := &shadowMemory{rows: []memory.Memory{{
		ID: id.MustParse("00000000-0000-4000-8000-000000000832"), StoryID: storyID,
		TimelineID: timelineID, OwnerType: memory.OwnerTimeline, OwnerID: id.ID(timelineID),
		ValidFromSeq: 1, Content: "old branch fact", Score: 0.9,
	}}}
	action := PlayerAction{StoryID: storyID, TimelineID: timelineID, ExpectedHead: 12, Text: "Продолжить"}

	baselineLLM := scripted()
	baseline := Pipeline{LLM: baselineLLM, Canon: &appender{head: 12}, Targets: targetSource{}}
	if _, err := baseline.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000833"), action); err != nil {
		t.Fatal(err)
	}

	shadowLLM := scripted()
	var metrics generationmetrics.Metrics
	shadow := Pipeline{
		LLM: shadowLLM, Canon: &appender{head: 12}, Targets: targetSource{},
		ContextShadow: ContextShadowBuilder{Builder: contextbuilder.Builder{Embeddings: fakeai.NewEmbeddingProvider(4), Memory: memoryStore}, TokenBudget: 1000, RetrievalLimit: 8},
		Metrics:       func(value generationmetrics.Metrics) { metrics = value },
	}
	if _, err := shadow.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000834"), action); err != nil {
		t.Fatal(err)
	}

	if metrics.ShadowRetrievedCount != 1 || metrics.ShadowRetrievalError != "" {
		t.Fatalf("shadow result was not measured: %#v", metrics)
	}
	if memoryStore.query.StoryID != storyID || memoryStore.query.TimelineID != timelineID || memoryStore.query.OwnerID != id.ID(timelineID) || memoryStore.query.AtEventSeq != 12 {
		t.Fatalf("shadow retrieval scope mismatch: %#v", memoryStore.query)
	}
	if string(requestForRole(t, baselineLLM.Requests, "writer").Input) != string(requestForRole(t, shadowLLM.Requests, "writer").Input) {
		t.Fatal("shadow context changed production Writer input")
	}
}

func requestForRole(t *testing.T, requests []ai.StoryRequest, role string) ai.StoryRequest {
	t.Helper()
	for _, request := range requests {
		if request.Role == role {
			return request
		}
	}
	t.Fatalf("role %s was not called", role)
	return ai.StoryRequest{}
}
