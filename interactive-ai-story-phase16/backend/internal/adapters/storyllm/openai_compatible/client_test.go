package openai_compatible

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestGenerateUsesOpenAICompatibleContractAndDoesNotLeakKeyIntoBody(t *testing.T) {
	var auth, body string
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"{\"intent\":\"inspect\"}"}}],"usage":{"prompt_tokens":10,"completion_tokens":4}}`)), Header: make(http.Header)}, nil
	})}
	c, e := New(Config{BaseURL: "http://127.0.0.1:8081", APIKey: "super-secret", Model: "baseline-model", Profile: "q5"}, hc)
	if e != nil {
		t.Fatal(e)
	}
	out, e := c.Generate(context.Background(), aiport.StoryRequest{Role: "action_interpreter", PromptVersion: "v1", Input: []byte(`{"action":"look"}`)})
	if e != nil {
		t.Fatal(e)
	}
	if string(out.Output) != `{"intent":"inspect"}` || out.InputTokens != 10 || out.OutputTokens != 4 {
		t.Fatalf("bad response %#v", out)
	}
	if auth != "Bearer super-secret" {
		t.Fatal("auth header missing")
	}
	if strings.Contains(body, "super-secret") {
		t.Fatal("API key leaked into request payload")
	}
	if c.Identity().Model != "baseline-model" {
		t.Fatal("model identity not pinned")
	}
}

func TestGeminiUsesGoogleOpenAICompatibleEndpointAndIdentity(t *testing.T) {
	var endpoint, body string
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		endpoint = r.URL.String()
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`)), Header: make(http.Header)}, nil
	})}
	c, err := New(Config{BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/", APIKey: "gemini-secret", Provider: "google_gemini", Model: "gemini-3.5-flash-lite", Profile: "gemini-free-tier"}, hc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Generate(context.Background(), aiport.StoryRequest{Role: "writer", PromptVersion: "v1", Input: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions" {
		t.Fatalf("unexpected Gemini endpoint: %s", endpoint)
	}
	if c.Identity().Provider != "google_gemini" || c.Identity().Model != "gemini-3.5-flash-lite" {
		t.Fatalf("unexpected Gemini identity: %#v", c.Identity())
	}
	if !strings.Contains(body, `"reasoning_effort":"low"`) {
		t.Fatalf("Gemini JSON request must use low reasoning effort: %s", body)
	}
}

func TestGemmaUsesMinimalReasoningEffort(t *testing.T) {
	var body string
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`)), Header: make(http.Header)}, nil
	})}
	c, err := New(Config{BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", APIKey: "gemini-secret", Provider: "google_gemini", Model: "gemma-4-31b-it"}, hc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Generate(context.Background(), aiport.StoryRequest{Role: "writer", PromptVersion: "v1", Input: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"reasoning_effort":"minimal"`) {
		t.Fatalf("Gemma request must disable expensive thinking: %s", body)
	}
}

func TestAntigravityUsesInteractionsAPI(t *testing.T) {
	var endpoint, apiKey, apiRevision, authorization, body string
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		endpoint = r.URL.String()
		apiKey = r.Header.Get("x-goog-api-key")
		apiRevision = r.Header.Get("Api-Revision")
		authorization = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		response := `{"status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"{\"ok\":true}"}]}],"usage":{"total_input_tokens":42,"total_output_tokens":7}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header)}, nil
	})}
	c, err := New(Config{BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", APIKey: "agent-secret", Provider: "google_gemini", Model: "antigravity-preview-05-2026"}, hc)
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Generate(context.Background(), aiport.StoryRequest{Role: "writer", PromptVersion: "v1", Input: []byte(`{"story":"continue"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://generativelanguage.googleapis.com/v1beta/interactions" || apiKey != "agent-secret" || apiRevision != "2026-05-20" || authorization != "" {
		t.Fatalf("unexpected Antigravity request: endpoint=%s apiKey=%q revision=%q authorization=%q", endpoint, apiKey, apiRevision, authorization)
	}
	if string(out.Output) != `{"ok":true}` || out.InputTokens != 42 || out.OutputTokens != 7 {
		t.Fatalf("unexpected Antigravity response: %#v", out)
	}
	if strings.Contains(body, "agent-secret") || !strings.Contains(body, `"agent":"antigravity-preview-05-2026"`) || !strings.Contains(body, `"environment":"remote"`) {
		t.Fatalf("unexpected Antigravity body: %s", body)
	}
}

func TestGeminiRotatesAndRemembersNextKeyAfterQuotaError(t *testing.T) {
	var auth []string
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		auth = append(auth, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") == "Bearer key-one" {
			return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader(`{"error":{"status":"RESOURCE_EXHAUSTED"}}`)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`)), Header: make(http.Header)}, nil
	})}
	c, err := New(Config{BaseURL: "https://rotation-test.invalid/v1beta/openai", APIKeys: []string{"key-one", "key-two"}, Provider: "google_gemini", Model: "gemini-3.5-flash-lite"}, hc)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = c.Generate(context.Background(), aiport.StoryRequest{Role: "writer", PromptVersion: "v1", Input: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"Bearer key-one", "Bearer key-two", "Bearer key-two"}
	if strings.Join(auth, ",") != strings.Join(want, ",") {
		t.Fatalf("credential rotation = %#v, want %#v", auth, want)
	}
}

func TestGeminiRotatesAfterEmbeddedQuotaErrorInSuccessfulHTTPResponse(t *testing.T) {
	var auth []string
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		auth = append(auth, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") == "Bearer embedded-key-one" {
			body := `[{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}]`
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`)), Header: make(http.Header)}, nil
	})}
	c, err := New(Config{BaseURL: "https://embedded-rotation.invalid/v1beta/openai", APIKeys: []string{"embedded-key-one", "embedded-key-two"}, Provider: "google_gemini", Model: "gemini-3.6-flash"}, hc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Generate(context.Background(), aiport.StoryRequest{Role: "writer", PromptVersion: "v1", Input: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	want := []string{"Bearer embedded-key-one", "Bearer embedded-key-two"}
	if strings.Join(auth, ",") != strings.Join(want, ",") {
		t.Fatalf("embedded credential rotation = %#v, want %#v", auth, want)
	}
}

func TestGeminiDoesNotRotateKeysForOrdinaryServerError(t *testing.T) {
	requests := 0
	hc := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"temporary"}}`)), Header: make(http.Header)}, nil
	})}
	c, _ := New(Config{BaseURL: "https://no-rotation.invalid/v1beta/openai", APIKeys: []string{"key-one", "key-two"}, Provider: "google_gemini", Model: "gemini-3.5-flash-lite"}, hc)
	_, err := c.Generate(context.Background(), aiport.StoryRequest{Role: "writer", PromptVersion: "v1", Input: []byte(`{}`)})
	if !errors.Is(err, aiport.ErrUnavailable) || requests != 1 {
		t.Fatalf("ordinary provider error must not rotate keys: requests=%d err=%v", requests, err)
	}
}
func TestRejectsNonJSONAssistantOutput(t *testing.T) {
	hc := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"not json"}}]}`)), Header: make(http.Header)}, nil
	})}
	c, _ := New(Config{BaseURL: "http://localhost:1", Model: "m"}, hc)
	_, e := c.Generate(context.Background(), aiport.StoryRequest{Role: "writer", Input: []byte(`{}`)})
	if !errors.Is(e, aiport.ErrInvalidOutput) {
		t.Fatalf("expected invalid output, got %v", e)
	}
}

func TestNormalizeJSONObjectAcceptsJSONFence(t *testing.T) {
	out, ok := normalizeJSONObject("```json\n{\"ok\":true}\n```")
	if !ok {
		t.Fatal("expected fenced JSON object to be accepted")
	}
	if string(out) != `{"ok":true}` {
		t.Fatalf("unexpected normalized output: %s", out)
	}
}

func TestNormalizeJSONObjectRejectsProseAroundJSON(t *testing.T) {
	_, ok := normalizeJSONObject("Here is the result:\n```json\n{\"ok\":true}\n```")
	if ok {
		t.Fatal("prose around JSON must not be accepted")
	}
}

func TestNormalizeJSONObjectRejectsArray(t *testing.T) {
	_, ok := normalizeJSONObject(`[{"ok":true}]`)
	if ok {
		t.Fatal("top-level array must not be accepted")
	}
}

func TestGenerateUsesRuntimePromptAndRoleSampling(t *testing.T) {
	var requestBody chatRequest
	var rawBody string
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		rawBody = string(b)
		if err := json.Unmarshal(b, &requestBody); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`)), Header: make(http.Header)}, nil
	})}
	runtime := promptset.Revision{Prompts: map[string]string{"writer": "RUNTIME WRITER CONTRACT"}, RoleSettings: map[string]promptset.RoleSettings{"writer": {Temperature: .83, TopP: .91, MaxTokens: 3456}}}
	c, err := New(Config{BaseURL: "http://localhost:8081", Model: "m", PromptSet: &runtime}, hc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Generate(context.Background(), aiport.StoryRequest{Role: "writer", PromptVersion: "v1", Input: []byte(`{"x":1}`)}); err != nil {
		t.Fatal(err)
	}
	if requestBody.Temperature != .83 || requestBody.TopP != .91 || requestBody.MaxTokens != 3456 {
		t.Fatalf("runtime sampling not applied: %#v", requestBody)
	}
	if !strings.Contains(rawBody, "RUNTIME WRITER CONTRACT") {
		t.Fatal("runtime prompt not sent")
	}
}

func TestGenerateCanNarrowRoleTokenBudgetPerRequest(t *testing.T) {
	var requestBody chatRequest
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`)), Header: make(http.Header)}, nil
	})}
	runtime := promptset.Revision{Prompts: map[string]string{"setup_architect": "contract"}, RoleSettings: map[string]promptset.RoleSettings{"setup_architect": {Temperature: .7, TopP: .8, MaxTokens: 8192}}}
	c, err := New(Config{BaseURL: "http://localhost:8081", Model: "m", PromptSet: &runtime}, hc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Generate(context.Background(), aiport.StoryRequest{Role: "setup_architect", Input: []byte(`{}`), MaxTokens: 1600}); err != nil {
		t.Fatal(err)
	}
	if requestBody.MaxTokens != 1600 {
		t.Fatalf("expected request token budget 1600, got %d", requestBody.MaxTokens)
	}
}
