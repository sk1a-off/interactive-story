package generation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	browser "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/openai_compatible"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

// Exercise the real adapter and HTTP admission, not just a provider identity mock.
func TestBrowserPipelineQueuesIndependentRolesAndJoinsBeforeCommit(t *testing.T) {
	responses := scripted().Responses
	started := make(chan string, 3)
	worldRelease := make(chan struct{})
	othersRelease := make(chan struct{})
	choicesStarted := make(chan struct{}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ResponseFormat struct {
				JSONSchema struct {
					Name string `json:"name"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		role := strings.TrimSuffix(request.ResponseFormat.JSONSchema.Name, "_v1")
		var release <-chan struct{}
		switch role {
		case "world_evaluator":
			started <- role
			release = worldRelease
		case "state_evaluator", "quest_evaluator":
			started <- role
			release = othersRelease
		case "choices":
			choicesStarted <- struct{}{}
		}
		if release != nil {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(responses[role])}}}})
	}))
	defer server.Close()
	client, err := browser.New(browser.Config{Provider: "chatgpt_browser", BaseURL: server.URL, Model: "gpt-6", APIKey: strings.Repeat("a", 48), JSONSchema: true}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	canon := &appender{}
	p := Pipeline{LLM: client, Canon: canon, Targets: targetSource{}}
	done := make(chan error, 1)
	go func() {
		_, err := p.Run(ctx, id.MustParse("00000000-0000-4000-8000-000000000030"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000031")), Text: "Осмотреть дверь"})
		done <- err
	}()
	seen := map[string]bool{}
	for len(seen) < 3 {
		select {
		case role := <-started:
			seen[role] = true
		case <-ctx.Done():
			t.Fatal("independent browser roles serialized", seen)
		}
	}
	select {
	case <-choicesStarted:
		t.Fatal("choices started before journal/quests")
	default:
	}
	close(othersRelease)
	select {
	case <-choicesStarted:
	case <-ctx.Done():
		t.Fatal("choices blocked on independent world branch")
	}
	select {
	case err := <-done:
		t.Fatal("turn committed before world completed", err)
	default:
	}
	close(worldRelease)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(canon.events) != 3 {
		t.Fatal("wrong Canon commit", len(canon.events))
	}
}

func TestBrowserEvaluatorFailureWaitsForSiblingsAndDoesNotCommit(t *testing.T) {
	l := newScheduleGateLLM()
	l.provider = "chatgpt_browser"
	l.gates["world_evaluator"] = make(chan struct{})
	l.errors["state_evaluator"] = ai.ErrUnavailable
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	canon := &appender{}
	done := startScheduledPipeline(ctx, l, canon)
	waitEvaluatorStarts(t, l)
	select {
	case <-l.failed:
	case <-ctx.Done():
		t.Fatal("failure did not reach evaluator")
	}
	select {
	case err := <-done:
		t.Fatal("returned while sibling still running", err)
	default:
	}
	close(l.gates["world_evaluator"])
	if err := awaitScheduleResult(t, done); err == nil {
		t.Fatal("failed evaluator accepted")
	}
	if len(canon.events) != 0 {
		t.Fatal("partial Canon commit")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active != 0 || l.completed["choices"] {
		t.Fatal("dependent choices or sibling leaked")
	}
}
