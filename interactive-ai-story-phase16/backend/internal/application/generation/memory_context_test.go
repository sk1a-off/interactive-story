package generation

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
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

type roleVisibility struct{ plan domain.Visibility }

func (r roleVisibility) Resolve(context.Context, id.ID, timeline.ID, int64) (domain.Visibility, error) {
	return r.plan, nil
}

type roleMemory struct {
	rows  []memory.Memory
	query memory.Query
	calls int
	err   error
}

func (r *roleMemory) Search(_ context.Context, q memory.Query) ([]memory.Memory, error) {
	r.query = q
	r.calls++
	return r.rows, r.err
}

func TestMemoryReachesOnlyPlanningAndWritingOncePerTurn(t *testing.T) {
	storyID := id.MustParse("00000000-0000-4000-8000-000000000910")
	tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000911"))
	action := PlayerAction{StoryID: storyID, TimelineID: tid, ExpectedHead: 12, Text: "осторожно открыть дверь"}
	plan, err := domain.ResolveVisibility(storyID, tid, 12, []domain.LineageNode{{StoryID: storyID, TimelineID: tid, HeadEventSeq: 12}})
	if err != nil {
		t.Fatal(err)
	}
	for _, planner := range []bool{false, true} {
		for _, failed := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "director", true: "planner"}[planner], map[bool]string{false: "enabled", true: "failure"}[failed]}, "/"), func(t *testing.T) {
				embeddings := fakeai.NewEmbeddingProvider(384)
				profile := memory.EmbeddingProfile{Provider: embeddings.ID.Provider, Model: embeddings.ID.Model, Revision: 1}
				row := memory.Memory{ID: id.MustParse("00000000-0000-4000-8000-000000000912"), StoryID: storyID, TimelineID: tid, OwnerType: memory.OwnerTimeline, OwnerID: id.ID(tid),
					ValidFromSeq: 1, Kind: "beat", DocumentClass: "historical_beat", SourceKey: "event:fixture", Content: "MEMORY_REFERENCE_ONLY", Score: 1, Profile: profile}
				rule := row
				rule.ID, _ = id.New()
				rule.Kind = "world_rule"
				rule.Content = "REMOVED_RULE_MUST_NOT_REACH_LLM"
				rule.SourceKey = "rule"
				rule.Score = 100
				private := row
				private.ID, _ = id.New()
				private.OwnerType = memory.OwnerCharacter
				private.Content = "PRIVATE_CHARACTER_MEMORY"
				private.SourceKey = "private"
				store := &roleMemory{rows: []memory.Memory{rule, private, row}}
				if failed {
					store.err = errors.New("sensitive internal error")
				}
				service := &memorycontext.Service{Builder: contextbuilder.Builder{Embeddings: embeddings, Memory: store, Visibility: roleVisibility{plan}, Profile: profile}, TopK: 8, MemoryTokenBudget: 2000}
				llm := scripted()
				baselineLLM := scripted()
				for _, model := range []*fakeai.ScriptedStoryLLM{llm, baselineLLM} {
					model.Responses["turn_planner"] = []byte(`{"intent":"осторожно открыть дверь","immediateGoal":"показать сопротивление замка","transition":"continue_scene","scene":{},"chapter":{},"riskFlags":[]}`)
				}
				target := staticTarget{target: generationtarget.Target{SceneID: id.MustParse("00000000-0000-4000-8000-000000000090"), NextBeatPosition: 2, SceneGoal: "SAVED_DIRECTOR_GOAL"}}
				p := Pipeline{LLM: llm, Canon: &appender{head: 12}, Targets: target, Memory: service}
				baseline := Pipeline{LLM: baselineLLM, Canon: &appender{head: 12}, Targets: target}
				if planner {
					p.Planner = LLMTurnPlanner{LLM: llm}
					baseline.Planner = LLMTurnPlanner{LLM: baselineLLM}
				}
				if _, err = p.Run(context.Background(), id.ID("00000000-0000-4000-8000-000000000913"), action); err != nil {
					t.Fatal(err)
				}
				if _, err = baseline.Run(context.Background(), id.ID("00000000-0000-4000-8000-000000000914"), action); err != nil {
					t.Fatal(err)
				}
				if embeddings.Calls != 1 || store.calls != 1 || !strings.Contains(store.query.Text, action.Text) || !strings.Contains(store.query.Text, "SAVED_DIRECTOR_GOAL") {
					t.Fatalf("lookups=%d embeddings=%d query=%+v", store.calls, embeddings.Calls, store.query)
				}
				if len(llm.Requests) != len(baselineLLM.Requests) {
					t.Fatal("memory added LLM calls")
				}
				for i, request := range llm.Requests {
					input := string(request.Input)
					want := !failed && (request.Role == "director" || request.Role == "writer" || request.Role == "turn_planner")
					if strings.Contains(input, "MEMORY_REFERENCE_ONLY") != want {
						t.Fatalf("role %s memory=%t want=%t", request.Role, strings.Contains(input, "MEMORY_REFERENCE_ONLY"), want)
					}
					if strings.Contains(input, "REMOVED_RULE_MUST_NOT_REACH_LLM") || strings.Contains(input, "PRIVATE_CHARACTER_MEMORY") || strings.Contains(input, "sensitive internal error") {
						t.Fatalf("forbidden memory in %s", request.Role)
					}
					if !want && input != string(baselineLLM.Requests[i].Input) {
						t.Fatalf("changed fallback/unrelated role %s", request.Role)
					}
				}
			})
		}
	}
}
