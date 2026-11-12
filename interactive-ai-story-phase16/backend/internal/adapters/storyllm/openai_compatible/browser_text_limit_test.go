package openai_compatible

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestBrowserTextLimitIsActionableAndNeverRetried(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("X-Story-Bridge-Limit-Scope", "text_characters")
		w.Header().Set("X-Story-Bridge-Request-Characters", "128506")
		w.Header().Set("X-Story-Bridge-Limit-Characters", "65536")
		http.Error(w, "text safety limit", 413)
	}))
	defer server.Close()
	c, err := New(Config{BaseURL: server.URL, Provider: "chatgpt_browser", Model: "gpt-6", APIKey: strings.Repeat("a", 48), BrowserRetries: 2}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Generate(context.Background(), ai.StoryRequest{Role: "director", PromptVersion: "v1", Input: []byte(`{}`)})
	if calls != 1 || err == nil || !strings.Contains(err.Error(), "65536") || !strings.Contains(err.Error(), "сократите контекст") {
		t.Fatal("wrong size handling", calls, err)
	}
}
