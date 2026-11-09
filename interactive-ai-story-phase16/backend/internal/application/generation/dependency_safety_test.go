package generation

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

type scheduleGateLLM struct {
	responses        map[string][]byte
	provider         string
	gates            map[string]chan struct{}
	started          chan string
	failed           chan string
	errors           map[string]error
	mu               sync.Mutex
	active           int
	completed        map[string]bool
	order            []string
	choicesInput     []byte
	prematureChoices bool
}

func newScheduleGateLLM() *scheduleGateLLM {
	return &scheduleGateLLM{responses: scripted().Responses, provider: "google_gemini", gates: map[string]chan struct{}{}, started: make(chan string, 16), failed: make(chan string, 3), errors: map[string]error{}, completed: map[string]bool{}}
}
func (l *scheduleGateLLM) Identity() ai.ProviderIdentity {
	return ai.ProviderIdentity{Provider: l.provider, Model: "schedule-test"}
}
func (l *scheduleGateLLM) Generate(ctx context.Context, r ai.StoryRequest) (ai.StoryResponse, error) {
	l.mu.Lock()
	l.active++
	l.order = append(l.order, r.Role)
	l.mu.Unlock()
	defer func() { l.mu.Lock(); l.active--; l.completed[r.Role] = true; l.mu.Unlock() }()
	l.started <- r.Role
	if gate := l.gates[r.Role]; gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ai.StoryResponse{}, ctx.Err()
		}
	}
	if err := l.errors[r.Role]; err != nil {
		l.failed <- r.Role
		return ai.StoryResponse{}, err
	}
	if r.Role == "choices" {
		l.mu.Lock()
		l.prematureChoices = !l.completed["state_evaluator"] || !l.completed["quest_evaluator"]
		l.choicesInput = append([]byte(nil), r.Input...)
		l.mu.Unlock()
	}
	return ai.StoryResponse{Output: append([]byte(nil), l.responses[r.Role]...)}, nil
}
func scheduleAction() PlayerAction {
	return PlayerAction{TimelineID: timeline.ID(mustID(2)), Text: "Герой поднимает ключ"}
}
func startScheduledPipeline(ctx context.Context, l *scheduleGateLLM, c *appender) <-chan error {
	result := make(chan error, 1)
	go func() {
		_, err := (Pipeline{LLM: l, Canon: c, Targets: targetSource{}}).Run(ctx, mustID(1), scheduleAction())
		result <- err
	}()
	return result
}
func waitEvaluatorStarts(t *testing.T, l *scheduleGateLLM) {
	t.Helper()
	seen := map[string]bool{}
	for len(seen) < 3 {
		select {
		case role := <-l.started:
			if role == "world_evaluator" || role == "state_evaluator" || role == "quest_evaluator" {
				seen[role] = true
			}
		case <-time.After(time.Second):
			t.Fatal("hosted evaluators did not all start")
		}
	}
}
func awaitScheduleResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("generation failed to terminate")
		return nil
	}
}
func TestHostedChoicesReceiveCompletedNormalizedJournalAndQuestResults(t *testing.T) {
	// Both real mutations must be delivered before choices. This makes their
	// dependency observable even if the other evaluator finishes first.
	for _, slow := range []string{"state_evaluator", "quest_evaluator"} {
		t.Run(slow, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			l := newScheduleGateLLM()
			l.gates[slow] = make(chan struct{})
			l.responses["writer"] = []byte(`{"text":"Герой поднял ключ и положил его в карман. Из башни снова прозвучал неизвестный сигнал."}`)
			l.responses["state_evaluator"] = []byte(`{"changes":[{"operation":"create","category":"item","name":"Ключ","quantity":1,"status":"active","evidenceQuote":"Герой поднял ключ и положил его в карман."}]}`)
			l.responses["quest_evaluator"] = []byte(`{"changes":[{"operation":"create","scope":"global","kind":"quest","title":"Раскрыть источник сигнала","successCriteria":"Источник сигнала найден и его назначение установлено","status":"active","progress":0,"evidenceQuote":"Из башни снова прозвучал неизвестный сигнал."}]}`)
			c := &appender{}
			result := startScheduledPipeline(ctx, l, c)
			waitEvaluatorStarts(t, l)
			close(l.gates[slow])
			if err := awaitScheduleResult(t, result); err != nil {
				t.Fatal(err)
			}
			if l.prematureChoices {
				t.Fatal("choices started before state/quest completed")
			}
			var input struct {
				Journal    []journalChange   `json:"journalChanges"`
				Objectives []objectiveChange `json:"objectiveChanges"`
			}
			if json.Unmarshal(l.choicesInput, &input) != nil || len(input.Journal) != 1 || len(input.Objectives) != 1 || input.Journal[0].EntryID == "" || input.Objectives[0].ObjectiveID == "" {
				t.Fatalf("choices missed validated mutations: %s", l.choicesInput)
			}
			if len(c.events) != 5 {
				t.Fatalf("unexpected committed events: %d", len(c.events))
			}
		})
	}
}
func TestHostedEvaluatorFailuresJoinInflightBranchesAndNeverCommit(t *testing.T) {
	for _, failedRole := range []string{"world_evaluator", "state_evaluator", "quest_evaluator"} {
		t.Run(failedRole, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			l := newScheduleGateLLM()
			for _, role := range []string{"world_evaluator", "state_evaluator", "quest_evaluator"} {
				l.gates[role] = make(chan struct{})
			}
			sentinel := errors.New("test evaluator failure")
			l.errors[failedRole] = sentinel
			c := &appender{}
			result := startScheduledPipeline(ctx, l, c)
			waitEvaluatorStarts(t, l)
			close(l.gates[failedRole])
			select {
			case <-l.failed:
			case <-time.After(time.Second):
				t.Fatal("failing branch did not return")
			}
			// A failed turn must retain ownership of its other provider requests until
			// they finish; otherwise they can race the next attempt's telemetry/state.
			select {
			case err := <-result:
				t.Fatalf("returned with blocked provider requests: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			for role, gate := range l.gates {
				if role != failedRole {
					close(gate)
				}
			}
			if err := awaitScheduleResult(t, result); !errors.Is(err, sentinel) {
				t.Fatalf("lost original failure: %v", err)
			}
			l.mu.Lock()
			active := l.active
			l.mu.Unlock()
			if active != 0 || len(c.events) != 0 {
				t.Fatalf("failed turn left work or Canon events: active=%d events=%d", active, len(c.events))
			}
		})
	}
}
func TestHostedCancellationDrainsBranchesWithoutCommitting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l := newScheduleGateLLM()
	for _, role := range []string{"world_evaluator", "state_evaluator", "quest_evaluator"} {
		l.gates[role] = make(chan struct{})
	}
	c := &appender{}
	result := startScheduledPipeline(ctx, l, c)
	waitEvaluatorStarts(t, l)
	cancel()
	if err := awaitScheduleResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	l.mu.Lock()
	active := l.active
	l.mu.Unlock()
	if active != 0 || len(c.events) != 0 {
		t.Fatalf("cancelled turn left work or Canon events: active=%d events=%d", active, len(c.events))
	}
}
func TestLocalProviderKeepsSequentialEvaluatorOrder(t *testing.T) {
	l := newScheduleGateLLM()
	l.provider = "openai_compatible"
	c := &appender{}
	result := startScheduledPipeline(context.Background(), l, c)
	if err := awaitScheduleResult(t, result); err != nil {
		t.Fatal(err)
	}
	var post []string
	for _, role := range l.order {
		if role == "world_evaluator" || role == "state_evaluator" || role == "quest_evaluator" || role == "choices" {
			post = append(post, role)
		}
	}
	want := []string{"world_evaluator", "state_evaluator", "quest_evaluator", "choices"}
	for i, role := range want {
		if len(post) != len(want) || post[i] != role {
			t.Fatalf("changed local scheduling: %v", post)
		}
	}
}

var _ ai.StoryLLM = (*scheduleGateLLM)(nil)
