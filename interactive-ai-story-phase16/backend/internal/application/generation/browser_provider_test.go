package generation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	browser "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/openai_compatible"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

func TestBrowserRetryCommitsOneTurnOrNothing(t *testing.T) {
	for _, exhausted := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovered", true: "exhausted"}[exhausted], func(t *testing.T) {
			source := scripted()
			writerCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					ResponseFormat struct {
						JSONSchema struct {
							Name string `json:"name"`
						} `json:"json_schema"`
					} `json:"response_format"`
				}
				if json.NewDecoder(r.Body).Decode(&request) != nil {
					t.Error("request decode failed")
					http.Error(w, "invalid", 400)
					return
				}
				role := strings.TrimSuffix(request.ResponseFormat.JSONSchema.Name, "_v1")
				if role == "writer" {
					writerCalls++
					if writerCalls == 1 || exhausted {
						w.Header().Set("X-Story-Bridge-State", "failed")
						w.Header().Set("X-Story-Bridge-Retryable", "true")
						http.Error(w, "invalid JSON", 502)
						return
					}
				}
				output, ok := source.Responses[role]
				if !ok {
					t.Error("unknown role")
					http.Error(w, "unknown role", 400)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(output)}}}})
			}))
			defer server.Close()
			client, err := browser.New(browser.Config{BaseURL: server.URL, Provider: "chatgpt_browser", Model: "gpt-6", APIKey: strings.Repeat("a", 48), BrowserRetries: 1, JSONSchema: true}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			canon := &appender{}
			sink := &memSink{}
			pipeline := Pipeline{LLM: client, Canon: canon, Sink: sink, Targets: targetSource{}, ProvisionalBeat: true}
			_, err = pipeline.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000010"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000011")), ExpectedHead: 0, Text: "Открыть дверь"})
			if writerCalls != 2 {
				t.Fatal("wrong call budget", writerCalls)
			}
			if exhausted {
				if err == nil || len(canon.events) != 0 {
					t.Fatal("invalid output reached Canon", err, len(canon.events))
				}
				return
			}
			if err != nil || len(canon.events) != 3 {
				t.Fatal("retry created missing or duplicate turn", err, len(canon.events))
			}
			beats := 0
			for _, event := range canon.events {
				if event.Type == "beat_committed" {
					beats++
				}
			}
			if beats != 1 {
				t.Fatal("duplicate story beat", beats)
			}
		})
	}
}
