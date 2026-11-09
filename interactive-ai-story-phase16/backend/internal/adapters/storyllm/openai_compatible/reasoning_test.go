package openai_compatible

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestReasoningOverrideIsRoleScopedImmutableAndOptIn(t *testing.T) {
	overrides := map[string]string{"state_evaluator": "minimal"}
	c, err := New(Config{BaseURL: "https://reasoning.test/openai", Provider: "google_gemini", Model: "gemini-3.5-flash-lite", RoleReasoningEffort: overrides}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		var body chatRequest
		_ = json.Unmarshal(raw, &body)
		want := "low"
		if strings.Contains(body.Messages[0].Content, "Hero Journal") {
			want = "minimal"
		}
		if body.ReasoningEffort != want {
			t.Fatalf("override leaked/lost: got %s want %s", body.ReasoningEffort, want)
		}
		return schemaHTTP(200, schemaChatOK), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	overrides["state_evaluator"] = "low"
	overrides["writer"] = "minimal"
	for _, role := range []string{"state_evaluator", "writer", "choices"} {
		response, err := c.Generate(context.Background(), ai.StoryRequest{Role: role, PromptVersion: "v1"})
		if err != nil {
			t.Fatal(err)
		}
		want := "low"
		if role == "state_evaluator" {
			want = "minimal"
		}
		if response.ReasoningEffort != want {
			t.Fatalf("effective effort missing: %+v", response)
		}
	}
}
func TestReasoningOverrideRejectsUnsupportedModelsAndValues(t *testing.T) {
	for _, tt := range []struct{ provider, model, role, effort string }{
		{"openai_compatible", "llamacpp", "writer", "minimal"},
		{"google_gemini", "antigravity-preview-05-2026", "writer", "minimal"},
		{"google_gemini", "gemma-4-31b-it", "choices", "low"},
		{"google_gemini", "gemini-3.5-flash-lite", "unknown", "minimal"},
		{"google_gemini", "gemini-3.5-flash-lite", "choices", "none"},
		{"google_gemini", "gemini-3.5-flash-lite", "choices", "high"},
	} {
		if _, err := New(Config{BaseURL: "https://unsupported.test", Provider: tt.provider, Model: tt.model, RoleReasoningEffort: map[string]string{tt.role: tt.effort}}, nil); err == nil {
			t.Fatalf("unsupported override accepted: %+v", tt)
		}
	}
	// Unsupported experiments must not change existing local/agent constructors.
	for _, model := range []string{"llamacpp", "antigravity-preview-05-2026", "gemma-4-31b-it"} {
		if _, err := New(Config{BaseURL: "https://default.test", Model: model}, nil); err != nil {
			t.Fatal(err)
		}
	}
}
func TestReasoningUsagePreservesMissingVersusReportedZero(t *testing.T) {
	for _, tt := range []struct {
		details string
		want    *int64
	}{
		{``, nil},
		{`,"completion_tokens_details":{"reasoning_tokens":"unknown"}`, nil},
		{`,"completion_tokens_details":{"reasoning_tokens":-1}`, nil},
		{`,"completion_tokens_details":{"reasoning_tokens":null}`, nil},
		{`,"completion_tokens_details":[]`, nil}, {`,"completion_tokens_details":{"reasoning_tokens":0}`, int64Pointer(0)}, {`,"completion_tokens_details":{"reasoning_tokens":7}`, int64Pointer(7)},
	} {
		c, _ := New(Config{BaseURL: "https://usage.test", Provider: "google_gemini", Model: "gemini-3.5-flash-lite"}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return schemaHTTP(200, `{"choices":[{"message":{"content":"{\"intent\":\"look\"}"}}],"usage":{"completion_tokens":9`+tt.details+`}}`), nil
		})})
		response, err := c.Generate(context.Background(), ai.StoryRequest{Role: "action_interpreter", PromptVersion: "v1"})
		if err != nil || response.OutputTokens != 9 {
			t.Fatal(err)
		}
		if (response.ReasoningTokens == nil) != (tt.want == nil) || (tt.want != nil && *response.ReasoningTokens != *tt.want) {
			t.Fatalf("reasoning usage fabricated/lost: %+v", response)
		}
	}
}
func int64Pointer(n int64) *int64 { return &n }
func TestReasoningSurvivesSchemaFallbackButNeverFallsBackItself(t *testing.T) {
	calls := 0
	c, _ := New(Config{BaseURL: "https://reasoning-schema.test", Provider: "google_gemini", Model: "gemini-3.5-flash-lite", JSONSchema: true, RoleReasoningEffort: map[string]string{"state_evaluator": "minimal"}}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		var body chatRequest
		_ = json.Unmarshal(raw, &body)
		if body.ReasoningEffort != "minimal" {
			t.Fatal("schema fallback changed reasoning treatment")
		}
		if calls == 1 {
			return schemaHTTP(400, `{"error":"schema unsupported"}`), nil
		}
		return schemaHTTP(200, schemaChatOK), nil
	})})
	response, err := c.Generate(context.Background(), ai.StoryRequest{Role: "state_evaluator", PromptVersion: "v1"})
	if err != nil || calls != 2 || !response.SchemaFallback || response.ReasoningEffort != "minimal" {
		t.Fatal("combined capability negotiation failed")
	}
	calls = 0
	c, _ = New(Config{BaseURL: "https://reasoning-rejected.test", Provider: "google_gemini", Model: "gemini-3.5-flash-lite", RoleReasoningEffort: map[string]string{"choices": "minimal"}}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return schemaHTTP(400, `{"error":"reasoning_effort minimal is not supported"}`), nil
	})})
	if _, err := c.Generate(context.Background(), ai.StoryRequest{Role: "choices", PromptVersion: "v1"}); err == nil || calls != 1 {
		t.Fatalf("unsupported effort was silently changed: %v calls=%d", err, calls)
	}
}
