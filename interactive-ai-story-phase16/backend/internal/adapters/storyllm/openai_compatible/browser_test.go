package openai_compatible

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserRetriesOnlyRejectedOrInvalidReplies(t *testing.T) {
	for _, mode := range []string{"rejected", "shape", "uncertain", "oversized", "exhausted"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var request chatRequest
				if json.NewDecoder(r.Body).Decode(&request) != nil {
					t.Error("bad request")
				}
				if request.ReasoningEffort != "high" || r.Header.Get("X-Story-Project") != "1" {
					t.Error("missing browser configuration")
				}
				if mode == "oversized" {
					http.Error(w, "too large", 413)
					return
				}
				if mode == "uncertain" {
					w.Header().Set("X-Story-Bridge-State", "uncertain")
					http.Error(w, "uncertain", 502)
					return
				}
				if (mode == "rejected" && calls == 1) || mode == "exhausted" {
					w.Header().Set("X-Story-Bridge-State", "failed")
					w.Header().Set("X-Story-Bridge-Retryable", "true")
					http.Error(w, "invalid", 502)
					return
				}
				content := `{"text":"Проверенный текст продолжения."}`
				if mode == "shape" && calls == 1 {
					content = `{"goal":"Это не ответ автора"}`
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
			}))
			defer server.Close()
			client, err := New(Config{BaseURL: server.URL, Provider: "chatgpt_browser", Model: "gpt-6", APIKey: strings.Repeat("a", 48), ReasoningEffort: "high", BrowserRetries: 1, JSONSchema: true}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			callbacks := 0
			response, err := client.Generate(context.Background(), ai.StoryRequest{Role: "writer", PromptVersion: "v1", Input: []byte(`{}`), OnText: func(string) { callbacks++ }})
			if callbacks != 0 {
				t.Fatal("unvalidated writer text leaked")
			}
			if mode == "oversized" {
				if !errors.Is(err, ai.ErrUnavailable) || calls != 1 || !strings.Contains(err.Error(), "лимит контекста") {
					t.Fatal("oversized request retried or hidden", calls, err)
				}
				return
			}

			if mode == "uncertain" {
				if err == nil || calls != 1 {
					t.Fatal("uncertain outcome resent", calls, err)
				}
				return
			}
			if calls != 2 || response.ProtocolRetries != 1 {
				t.Fatal("retry budget incorrect", calls, response.ProtocolRetries)
			}
			if mode == "exhausted" {
				if !errors.Is(err, ai.ErrInvalidOutput) {
					t.Fatal(err)
				}
				return
			}
			if err != nil || !response.StreamFallback || !strings.Contains(string(response.Output), "Проверенный") {
				t.Fatal(response, err)
			}
		})
	}
}
func TestBrowserCancelledCallNeverSends(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, `{}`) }))
	defer s.Close()
	c, err := New(Config{BaseURL: s.URL, Provider: "chatgpt_browser", Model: "gpt-6", APIKey: strings.Repeat("a", 48), BrowserRetries: 2}, s.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Generate(ctx, ai.StoryRequest{Role: "writer", PromptVersion: "v1", Input: []byte(`{}`)})
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal(calls, err)
	}
}
func TestBrowserResponseShapes(t *testing.T) {
	for _, tc := range []struct {
		role, reply string
		valid       bool
	}{
		{"writer", `{"text":"Продолжение"}`, true}, {"writer", `{"text":" "}`, false}, {"writer", `{"goal":"не тот контракт"}`, false},
		{"world_evaluator", `{"changes":[]}`, true}, {"state_evaluator", `{"changes":[{"operation":"update","evidenceQuote":"Израсходовала паёк","quantity":-1}]}`, false},
		{"quest_evaluator", `{"changes":[{"operation":"update","evidenceQuote":"Достигла цели","progress":101}]}`, false},
		{"turn_planner", `{"intent":"Осмотреть","immediateGoal":"Проверить","transition":"teleport"}`, false},
		{"choices", `{"choices":[{"id":"a","label":"Один"},{"id":"a","label":"Два"},{"id":"c","label":"Три"},{"id":"d","label":"Четыре"}]}`, false},
		{"setup_editor", `{"patch":{"title":"История"}}`, true},
	} {
		err := validateBrowserRole(tc.role, []byte(`{}`), []byte(tc.reply))
		if (err == nil) != tc.valid {
			t.Errorf("%s %s: %v", tc.role, tc.reply, err)
		}
	}
}

func TestBrowserDoesNotRedirectStoryPayload(t *testing.T) {
	forwarded := 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded++ }))
	defer target.Close()
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer bridge.Close()
	c, err := New(Config{BaseURL: bridge.URL, Provider: "chatgpt_browser", Model: "gpt-6", APIKey: strings.Repeat("a", 48), BrowserRetries: 2}, bridge.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Generate(context.Background(), ai.StoryRequest{Role: "director", PromptVersion: "v1", Input: []byte(`{"intent":"private synthetic input"}`)})
	if err == nil || forwarded != 0 {
		t.Fatal("browser payload followed redirect", err, forwarded)
	}
}

func TestBrowserTransportPreservesForegroundAndImagePriority(t *testing.T) {
	for _, role := range []string{"writer", "image_prompt"} {
		t.Run(role, func(t *testing.T) {
			priority := "unset"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				priority = r.Header.Get("X-Story-Priority")
				fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"text\":\"Готовый ответ\"}"}}]}`)
			}))
			defer server.Close()
			client, err := New(Config{BaseURL: server.URL, Provider: "chatgpt_browser", Model: "gpt-6", APIKey: strings.Repeat("a", 48), PromptSet: &promptset.Revision{Prompts: map[string]string{role: "Return JSON"}}}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Generate(context.Background(), ai.StoryRequest{Role: role, PromptVersion: "v1", Input: []byte(`{}`)}); err != nil {
				t.Fatal(err)
			}
			expected := ""
			if role == "image_prompt" {
				expected = "background"
			}
			if priority != expected {
				t.Fatal("wrong queue priority", role, priority)
			}
		})
	}
}
