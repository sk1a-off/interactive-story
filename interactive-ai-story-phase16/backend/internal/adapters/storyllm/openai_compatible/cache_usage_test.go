package openai_compatible

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestCachedUsageBothProtocolsMissingZeroAndMalformed(t *testing.T) {
	for _, agent := range []bool{false, true} {
		for _, tt := range []struct {
			raw  string
			want *int64
		}{
			{``, nil}, {`0`, int64Pointer(0)}, {`123`, int64Pointer(123)}, {`null`, nil}, {`-1`, nil}, {`"unknown"`, nil}, {`[]`, nil},
		} {
			model, role := "gemini-3.5-flash-lite", "action_interpreter"
			if agent {
				model, role = "antigravity-preview-05-2026", "writer"
			}
			c, err := New(Config{BaseURL: "https://cache-usage.test/openai", Provider: "google_gemini", Model: model}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				var body map[string]any
				if agent {
					_ = json.Unmarshal([]byte(schemaAgentOK), &body)
				} else {
					_ = json.Unmarshal([]byte(schemaChatOK), &body)
				}
				usage := map[string]any{}
				if agent {
					usage["total_input_tokens"], usage["total_output_tokens"] = 1000, 9
					if tt.raw != "" {
						usage["total_cached_tokens"] = json.RawMessage(tt.raw)
					}
				} else {
					usage["prompt_tokens"], usage["completion_tokens"] = 1000, 9
					if tt.raw != "" {
						usage["prompt_tokens_details"] = map[string]any{"cached_tokens": json.RawMessage(tt.raw)}
					}
				}
				body["usage"] = usage
				raw, _ := json.Marshal(body)
				return schemaHTTP(200, string(raw)), nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			response, err := c.Generate(context.Background(), ai.StoryRequest{Role: role, PromptVersion: "v1"})
			if err != nil || response.InputTokens != 1000 || response.OutputTokens != 9 {
				t.Fatal("cache telemetry changed valid output or total usage")
			}
			if (response.CachedInputTokens == nil) != (tt.want == nil) || tt.want != nil && *response.CachedInputTokens != *tt.want {
				t.Fatalf("cache zero/absence conflated: agent=%t raw=%s", agent, tt.raw)
			}
		}
	}
}
func TestCacheTelemetryNeverReordersRuntimePrompts(t *testing.T) {
	input := []byte(`{"action":"DYNAMIC_FIRST","context":{"storyBible":{"title":"STABLE"}}}`)
	c, _ := New(Config{BaseURL: "http://local.test", Model: "llamacpp"}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		var body chatRequest
		_ = json.Unmarshal(raw, &body)
		if len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[1].Role != "user" || body.Messages[1].Content != string(input) {
			t.Fatalf("runtime prefix changed: %s", raw)
		}
		return schemaHTTP(200, schemaChatOK), nil
	})})
	if _, err := c.Generate(context.Background(), ai.StoryRequest{Role: "action_interpreter", PromptVersion: "v1", Input: input}); err != nil {
		t.Fatal(err)
	}
}
func TestMalformedPromptUsageDoesNotRejectValidStory(t *testing.T) {
	c, _ := New(Config{BaseURL: "https://malformed-cache.test", Model: "local"}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return schemaHTTP(200, `{"choices":[{"message":{"content":"{\"text\":\"Продолжение\"}"}}],"usage":{"prompt_tokens_details":[]}}`), nil
	})})
	response, err := c.Generate(context.Background(), ai.StoryRequest{Role: "writer", PromptVersion: "v1"})
	if err != nil || response.CachedInputTokens != nil {
		t.Fatal("optional usage became a generation failure")
	}
}
