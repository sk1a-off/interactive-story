package openai_compatible

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

// A strict local wire fixture rejects hosted-provider extensions. This is a
// protocol regression check, not a substitute for running the selected GGUF.
func TestLocalContractAcrossEndpointShapesAndStreamModes(t *testing.T) {
	for _, suffix := range []string{"", "/v1", "/v1/", "/v1/chat/completions"} {
		for _, mode := range []string{"buffered", "stream", "ignored-stream"} {
			t.Run(suffix+"/"+mode, func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Header.Get("x-goog-api-key") != "" || r.Header.Get("Api-Revision") != "" {
						t.Errorf("local protocol changed: %s %s", r.Method, r.URL.Path)
					}
					var payload map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					allowed := map[string]bool{"model": true, "messages": true, "temperature": true, "top_p": true, "max_tokens": true, "response_format": true, "stream": true, "stream_options": true}
					for key := range payload {
						if !allowed[key] {
							t.Errorf("unrequested extension in local payload: %s", key)
							w.WriteHeader(400)
							return
						}
					}
					if string(payload["model"]) != `"local-gguf"` || string(payload["response_format"]) != `{"type":"json_object"}` {
						t.Error("model or default JSON contract changed")
					}
					stream := string(payload["stream"]) == "true"
					if stream != (mode != "buffered") {
						t.Error("streaming was not explicitly selected")
					}
					if mode == "stream" {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, chatSSE(`{"text":"Продолжение"}`, "stop")+"data: [DONE]\n\n")
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"text\":\"Продолжение\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
				}))
				defer server.Close()
				c, err := New(Config{BaseURL: server.URL + suffix, Model: "local-gguf", Profile: "llamacpp-16k-kvq8"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				req := ai.StoryRequest{Role: "writer", PromptVersion: "v1", Input: []byte(`{"action":"Продолжить"}`)}
				if mode != "buffered" {
					req.OnText = func(string) {}
				}
				result, err := c.Generate(context.Background(), req)
				if err != nil || calls != 1 || !strings.Contains(string(result.Output), "Продолжение") {
					t.Fatalf("local generation failed: calls=%d err=%v", calls, err)
				}
				if result.Provider.Model != "local-gguf" || result.Provider.Profile != "llamacpp-16k-kvq8" || result.ReasoningEffort != "" || result.Streamed != (mode == "stream") || result.StreamFallback != (mode == "ignored-stream") {
					t.Fatalf("local identity or mode changed: %+v", result)
				}
			})
		}
	}
}
