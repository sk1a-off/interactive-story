package imagegeneration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	"github.com/local/interactive-ai-story/backend/internal/application/contextbuilder"
	"github.com/local/interactive-ai-story/backend/internal/application/memorycontext"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/memory"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/imagegenerationrepo"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

type visualVisibility struct{ plan domain.Visibility }

func (r visualVisibility) Resolve(context.Context, id.ID, timeline.ID, int64) (domain.Visibility, error) {
	return r.plan, nil
}

type visualMemory struct {
	row   memory.Memory
	query memory.Query
	calls int
	err   error
}

type visualCheckpoint struct {
	calls int
	seq   *int64
	err   error
}

func (r *visualCheckpoint) ResolveMemoryCheckpoint(context.Context, id.ID, id.ID, id.ID, id.ID) (*int64, error) {
	r.calls++
	return r.seq, r.err
}

func (s *visualMemory) Search(_ context.Context, q memory.Query) ([]memory.Memory, error) {
	s.calls++
	s.query = q
	return []memory.Memory{s.row}, s.err
}

func TestVisualMemoryUsesBeatCheckpointAndSurvivesRepairsAndFallback(t *testing.T) {
	for _, mode := range []string{"single-repair", "paragraph-repair", "moments-fallback", "unavailable", "legacy", "resolved", "checkpoint-failure"} {
		t.Run(mode, func(t *testing.T) {
			story, _ := id.New()
			tid, _ := id.New()
			at := int64(5)
			plan, err := domain.ResolveVisibility(story, timeline.ID(tid), at, []domain.LineageNode{{StoryID: story, TimelineID: timeline.ID(tid), HeadEventSeq: 20}})
			if err != nil {
				t.Fatal(err)
			}
			e := fakeai.NewEmbeddingProvider(384)
			profile := memory.EmbeddingProfile{Provider: e.ID.Provider, Model: e.ID.Model, Revision: 1}
			rowID, _ := id.New()
			store := &visualMemory{row: memory.Memory{ID: rowID, StoryID: story, TimelineID: timeline.ID(tid), OwnerType: memory.OwnerTimeline, OwnerID: tid, ValidFromSeq: 1, Kind: "beat", DocumentClass: "historical_beat", SourceKey: "event:visual", Content: "VISUAL_MEMORY_EVIDENCE", Profile: profile}}
			if mode == "unavailable" {
				store.err = errors.New("private database error")
			}
			valid := []byte(`{"shouldIllustrate":true,"importance":1,"visualSummary":"A lone traveler approaches the ancient stone tower","positivePrompt":"a lone traveler approaching an ancient stone tower under pale moonlight, detailed cinematic fantasy illustration with natural anatomy and warm candlelight","negativePrompt":"text, captions"}`)
			outputs := [][]byte{[]byte(`{}`), valid}
			if mode == "moments-fallback" {
				outputs = [][]byte{[]byte(`{"moments":[]}`), valid}
			}
			llm := &visualLLM{outputs: outputs}
			service := &memorycontext.Service{Builder: contextbuilder.Builder{Embeddings: e, Memory: store, Visibility: visualVisibility{plan}, Profile: profile}, TopK: 8, MemoryTokenBudget: 2000}
			b := LLMPromptBuilder{LLMFactory: func(context.Context) (ai.StoryLLM, id.ID, error) { return llm, "", nil }, Memory: service}
			s := repo.SceneSource{StoryID: story, TimelineID: tid, Text: "Путник подходит к башне.", Goal: "reach tower", AtEventSeq: &at, VisualBible: `{"appearance":"CURRENT_VISUAL_PROFILE"}`}
			if mode == "legacy" {
				s.AtEventSeq = nil
			}
			checkpoint := &visualCheckpoint{seq: &at}
			if mode == "resolved" || mode == "checkpoint-failure" {
				s.AtEventSeq = nil
				b.MemoryCheckpoints = checkpoint
				if mode == "checkpoint-failure" {
					checkpoint.err = errors.New("private checkpoint error")
				}
			}
			switch mode {
			case "paragraph-repair":
				_, err = b.BuildForParagraph(context.Background(), s, 1)
			case "moments-fallback":
				_, err = b.BuildMoments(context.Background(), s, true)
			default:
				_, err = b.Build(context.Background(), s, true)
			}
			if err != nil {
				t.Fatal(err)
			}
			want := mode != "legacy" && mode != "unavailable" && mode != "checkpoint-failure"
			for _, request := range llm.requests {
				if strings.Contains(string(request.Input), "VISUAL_MEMORY_EVIDENCE") != want {
					t.Fatalf("memory lost/leaked in %s: %s", request.Role, request.Input)
				}
				if !strings.Contains(string(request.Input), "CURRENT_VISUAL_PROFILE") {
					t.Fatal("current appearance removed")
				}
			}
			if mode == "legacy" || mode == "checkpoint-failure" {
				if store.calls != 0 || e.Calls != 0 {
					t.Fatal("legacy beat triggered unbounded retrieval")
				}
			} else if store.calls != 1 || e.Calls != 1 || store.query.AtEventSeq != at {
				t.Fatalf("lookup/checkpoint mismatch: %+v calls=%d", store.query, store.calls)
			}
			if (mode == "resolved" || mode == "checkpoint-failure") && checkpoint.calls != 1 {
				t.Fatal("checkpoint looked up repeatedly")
			}
		})
	}
}
