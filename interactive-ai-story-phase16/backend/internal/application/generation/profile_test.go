package generation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/role_router"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	domainjob "github.com/local/interactive-ai-story/backend/internal/domain/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	"github.com/local/interactive-ai-story/backend/internal/ports/jobqueue"
)

func TestProfilesRunProductionPipelineWithExpectedRoleCounts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile domain.GenerationProfile
		legacy  bool
		calls   int
		planner bool
	}{
		{"old config, old chain", "", false, 8, false},
		{"quality, old chain", domain.GenerationQuality, false, 8, false},
		{"old config, planner enabled", "", true, 6, true},
		{"quality, planner enabled", domain.GenerationQuality, true, 6, true},
		{"balance, planner switch off", domain.GenerationBalance, false, 6, true},
		{"balance, planner switch on", domain.GenerationBalance, true, 6, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			llm := scripted()
			llm.Responses["turn_planner"] = []byte(`{"intent":"Герой поднимает ключ","immediateGoal":"Получить ключ","transition":"continue_scene"}`)
			planner, err := PlannerForProfile(tc.profile, tc.legacy, llm)
			if err != nil {
				t.Fatal(err)
			}
			canon := &appender{}
			_, err = (Pipeline{LLM: llm, Planner: planner, Canon: canon, Targets: targetSource{}, MaxRepairs: 1}).Run(context.Background(), mustID(1), scheduleAction())
			if err != nil || len(canon.events) == 0 || len(llm.Calls) != tc.calls {
				t.Fatalf("calls=%v Canon=%d err=%v", llm.Calls, len(canon.events), err)
			}
			counts := map[string]int{}
			for _, role := range llm.Calls {
				counts[role]++
			}
			for _, role := range []string{"writer", "world_evaluator", "state_evaluator", "quest_evaluator", "choices"} {
				if counts[role] != 1 {
					t.Fatalf("profile changed required role %s: %v", role, counts)
				}
			}
			if (counts["turn_planner"] == 1) != tc.planner || counts["lore_guard"] != 0 {
				t.Fatalf("incorrect planner or removed role: %v", counts)
			}
		})
	}
}

func TestBalanceRejectsRepeatedProseAfterOneRewrite(t *testing.T) {
	old := "Илья остановился у заросшего окна и внимательно посмотрел на пустую дорогу. Ветер раскачивал сухую ветку, а за ставнями никто не отвечал. Он ещё раз проверил засов и вернулся к спутнице."
	llm := scripted()
	llm.Responses["turn_planner"] = []byte(`{"intent":"Герой поднимает ключ","immediateGoal":"Получить ключ","transition":"continue_scene"}`)
	llm.Responses["writer"] = []byte(`{"text":"` + old + `"}`)
	planner, _ := PlannerForProfile(domain.GenerationBalance, false, llm)
	target, _ := (targetSource{}).CurrentWriteTarget(context.Background(), "")
	target.PreviousBeatText = old
	canon := &appender{}
	_, err := (Pipeline{LLM: llm, Planner: planner, Canon: canon, Targets: staticTarget{target: target}, MaxRepairs: 1}).Run(context.Background(), mustID(1), scheduleAction())
	writerCalls := 0
	for _, role := range llm.Calls {
		if role == "writer" {
			writerCalls++
		}
	}
	if err == nil || writerCalls != 2 || len(canon.events) != 0 {
		t.Fatalf("unbounded or committed failed rewrite: writers=%d Canon=%d err=%v", writerCalls, len(canon.events), err)
	}
}

func TestUnavailableProfilesFailBeforePipeline(t *testing.T) {
	for _, profile := range []domain.GenerationProfile{domain.GenerationSpeed, "unknown"} {
		llm := scripted()
		if _, err := PlannerForProfile(profile, true, llm); !errors.Is(err, domain.ErrInvalidPublicSettings) || len(llm.Calls) != 0 {
			t.Fatalf("unavailable profile started model work: %v", err)
		}
	}
}

func TestBalanceKeepsSafeHybridRoutingAndFastStateSelection(t *testing.T) {
	for _, fastState := range []bool{false, true} {
		fast, quality := scripted(), scripted()
		fast.Responses["turn_planner"] = []byte(`{"intent":"Герой поднимает ключ","immediateGoal":"Получить ключ","transition":"continue_scene"}`)
		quality.ID.Provider = "google_gemini"
		router, err := role_router.NewSafeHybridWithFastState(fast, quality, fastState)
		if err != nil {
			t.Fatal(err)
		}
		planner, _ := PlannerForProfile(domain.GenerationBalance, false, router)
		canon := &appender{}
		_, err = (Pipeline{LLM: router, Planner: planner, Canon: canon, Targets: targetSource{}, MaxRepairs: 1}).Run(context.Background(), mustID(1), scheduleAction())
		if err != nil {
			t.Fatal(err)
		}
		for _, request := range fast.Requests {
			if request.Role != "turn_planner" && request.Role != "choices" && !(fastState && request.Role == "state_evaluator") {
				t.Fatalf("profile rerouted a quality role: %s", request.Role)
			}
		}
		for _, request := range quality.Requests {
			if request.Role != "writer" && request.Role != "world_evaluator" && request.Role != "quest_evaluator" && !(!fastState && request.Role == "state_evaluator") {
				t.Fatalf("profile rerouted a fast role: %s", request.Role)
			}
		}
		wantFast := 2
		if fastState {
			wantFast++
		}
		if len(fast.Requests) != wantFast || len(quality.Requests) != 6-wantFast {
			t.Fatalf("routing changed: fast=%d quality=%d", len(fast.Requests), len(quality.Requests))
		}
	}
}

type pinnedProfileRepo struct {
	cfgRepo
	active domain.Revision
}

func (r pinnedProfileRepo) Active(context.Context) (domain.Revision, error) { return r.active, nil }
func (r pinnedProfileRepo) Get(_ context.Context, requested id.ID) (domain.Revision, error) {
	if requested != r.v.ID {
		return domain.Revision{}, errors.New("wrong pinned revision requested")
	}
	return r.v, nil
}

func TestQueuedGenerationUsesPinnedProfileAfterActiveSwitch(t *testing.T) {
	old := domain.Revision{ID: mustID(3), Settings: json.RawMessage(`{"generationProfile":"quality"}`)}
	active := domain.Revision{ID: mustID(4), Settings: json.RawMessage(`{"generationProfile":"balance"}`)}
	llm := scripted() // no planner response: accidentally using balance must fail
	canon := &appender{}
	executor := DurableExecutor{Config: pinnedProfileRepo{cfgRepo: cfgRepo{v: old}, active: active}, Hub: NewHub(), Resolve: func(_ context.Context, revision domain.Revision, _ *promptset.Revision) (Pipeline, error) {
		var settings domain.PublicSettings
		if err := json.Unmarshal(revision.Settings, &settings); err != nil {
			return Pipeline{}, err
		}
		planner, err := PlannerForProfile(settings.GenerationProfile, false, llm)
		return Pipeline{LLM: llm, Planner: planner, Canon: canon, Targets: targetSource{}}, err
	}}
	payload, _ := json.Marshal(scheduleAction())
	err := executor.Execute(context.Background(), jobqueue.ClaimedJob{Job: domainjob.Job{ID: mustID(1), ConfigRevisionID: old.ID, RequestPayload: payload}})
	if err != nil || len(llm.Calls) != 8 || len(canon.events) == 0 {
		t.Fatalf("queued job lost pinned quality profile: calls=%v err=%v", llm.Calls, err)
	}
}
