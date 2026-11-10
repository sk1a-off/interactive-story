package openai_compatible

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func schemaHTTP(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

const schemaChatOK = `{"choices":[{"message":{"content":"{\"intent\":\"inspect\"}"}}]}`
const schemaAgentOK = `{"status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"{\"text\":\"Продолжение\"}"}]}]}`

func TestSchemaOptInUsesRoleShapeForBothProtocols(t *testing.T) {
	for _, agent := range []bool{false, true} {
		t.Run(map[bool]string{false: "chat", true: "agent"}[agent], func(t *testing.T) {
			model, role := "gemini-3.5-flash-lite", "action_interpreter"
			if agent {
				model, role = "antigravity-preview-05-2026", "writer"
			}
			hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				raw, _ := io.ReadAll(r.Body)
				var body map[string]json.RawMessage
				_ = json.Unmarshal(raw, &body)
				var format struct {
					Type, MIMEType string
					JSONSchema     *jsonSchemaEnvelope `json:"json_schema"`
					Schema         map[string]any      `json:"schema"`
				}
				_ = json.Unmarshal(body["response_format"], &format)
				if agent {
					if format.Type != "text" || format.Schema == nil {
						t.Fatalf("missing agent schema: %s", raw)
					}
					return schemaHTTP(200, schemaAgentOK), nil
				}
				if format.Type != "json_schema" || format.JSONSchema == nil || format.JSONSchema.Strict {
					t.Fatalf("missing optional-field schema: %s", raw)
				}
				return schemaHTTP(200, schemaChatOK), nil
			})}
			c, _ := New(Config{BaseURL: "https://example.test/v1beta/openai", Provider: "google_gemini", Model: model, JSONSchema: true}, hc)
			response, err := c.Generate(context.Background(), ai.StoryRequest{Role: role, PromptVersion: "v1", Input: []byte(`{}`)})
			if err != nil || response.ResponseFormat != "json_schema" || response.SchemaFallback {
				t.Fatalf("format not reported: %+v %v", response, err)
			}
		})
	}
}
func TestSchemaRejectionFallsBackOnceAndCachesPerClient(t *testing.T) {
	for _, agent := range []bool{false, true} {
		t.Run(map[bool]string{false: "chat", true: "agent"}[agent], func(t *testing.T) {
			model, role := "gemini-3.5-flash-lite", "action_interpreter"
			if agent {
				model, role = "antigravity-preview-05-2026", "writer"
			}
			calls := 0
			var formats []bool
			hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				raw, _ := io.ReadAll(r.Body)
				formats = append(formats, strings.Contains(string(raw), `"schema":`))
				if calls == 1 {
					return schemaHTTP(400, `{"error":{"message":"response_format schema is not supported PRIVATE_PROVIDER_ERROR"}}`), nil
				}
				if r.Header.Get("Authorization") != "Bearer key-one" && r.Header.Get("x-goog-api-key") != "key-one" {
					t.Fatal("schema fallback rotated identity")
				}
				if agent {
					return schemaHTTP(200, schemaAgentOK), nil
				}
				return schemaHTTP(200, schemaChatOK), nil
			})}
			c, _ := New(Config{BaseURL: "https://schema-cache.test/openai", Provider: "google_gemini", Model: model, APIKeys: []string{"key-one", "key-two"}, JSONSchema: true}, hc)
			for i := 0; i < 2; i++ {
				r, err := c.Generate(context.Background(), ai.StoryRequest{Role: role, PromptVersion: "v1", Input: []byte(`{}`)})
				if err != nil || r.ResponseFormat != "json_object" || r.SchemaFallback != (i == 0) {
					t.Fatalf("invalid bounded fallback: %+v %v", r, err)
				}
			}
			if calls != 3 || !formats[0] || formats[1] || formats[2] {
				t.Fatalf("unexpected protocol attempts: %v", formats)
			}
		})
	}
}
func TestSchemaFallbackDoesNotHideOtherFailures(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{"invalid schema", 400, `{"error":"schema required must be an array"}`},
		{"ordinary invalid argument", 400, `{"error":"invalid model"}`},
		{"auth", 401, `{"error":"schema unsupported"}`},
		{"quota", 429, `{"error":"schema unsupported"}`},
		{"server", 500, `{"error":"schema unsupported"}`},
		{"invalid output", 200, `{"choices":[{"message":{"content":"not JSON"}}]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			c, _ := New(Config{BaseURL: "http://local.test/v1", Model: "local", JSONSchema: true, Requests: immediateControl()}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { calls++; return schemaHTTP(tt.status, tt.body), nil })})
			_, err := c.Generate(context.Background(), ai.StoryRequest{Role: "writer", PromptVersion: "v1"})
			wantCalls := 1
			if tt.status == 429 {
				wantCalls = 1 + maxQuotaRetries
			}
			if err == nil || calls != wantCalls || strings.Contains(err.Error(), "unsupported") {
				t.Fatalf("masked failure or raw body leak: calls=%d err=%v", calls, err)
			}
		})
	}
	// A rejected fallback is bounded too, even when the endpoint rejects both modes.
	calls := 0
	c, _ := New(Config{BaseURL: "http://reject.test", Model: "local", JSONSchema: true}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return schemaHTTP(422, `{"error":"response_format unsupported"}`), nil
	})})
	if _, err := c.Generate(context.Background(), ai.StoryRequest{Role: "writer", PromptVersion: "v1"}); err == nil || calls != 2 {
		t.Fatalf("unbounded fallback: %d %v", calls, err)
	}
}
func TestSchemaFallbackRecognizesEmbeddedGoogleRejection(t *testing.T) {
	calls := 0
	c, _ := New(Config{BaseURL: "https://embedded.test/openai", Model: "gemini-3.5-flash-lite", Provider: "google_gemini", JSONSchema: true}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return schemaHTTP(200, `[{"error":{"code":400,"message":"Unknown name schema in response_format"}}]`), nil
		}
		return schemaHTTP(200, schemaChatOK), nil
	})})
	r, err := c.Generate(context.Background(), ai.StoryRequest{Role: "action_interpreter", PromptVersion: "v1"})
	if err != nil || !r.SchemaFallback || calls != 2 {
		t.Fatalf("embedded rejection missed: %v %+v", err, r)
	}
}
func TestSchemaDefaultOffUnknownRolesAndDynamicRepair(t *testing.T) {
	for _, tt := range []struct {
		role, input   string
		enabled, want bool
	}{
		{"writer", `{}`, false, false}, {"setup_assistant", `{}`, true, false},
		{"structured_repair", `{"role":"writer"}`, true, true}, {"structured_repair", `{"role":"unknown"}`, true, false},
	} {
		t.Run(tt.role+tt.input, func(t *testing.T) {
			c, _ := New(Config{BaseURL: "http://local.test", Model: "llamacpp", JSONSchema: tt.enabled}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				raw, _ := io.ReadAll(r.Body)
				if strings.Contains(string(raw), `"json_schema"`) != tt.want {
					t.Fatalf("wrong schema gating: %s", raw)
				}
				return schemaHTTP(200, schemaChatOK), nil
			})})
			if _, err := c.Generate(context.Background(), ai.StoryRequest{Role: tt.role, PromptVersion: "v1", Input: []byte(tt.input)}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestSchemaFallbackStateIsConcurrentAndTransportErrorsDoNotRetry(t *testing.T) {
	c, _ := New(Config{BaseURL: "http://concurrent.test", Model: "local", JSONSchema: true}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), `"json_schema"`) {
			return schemaHTTP(400, `{"error":"schema unsupported"}`), nil
		}
		return schemaHTTP(200, schemaChatOK), nil
	})})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Generate(context.Background(), ai.StoryRequest{Role: "action_interpreter", PromptVersion: "v1"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	calls := 0
	c, _ = New(Config{BaseURL: "http://transport.test", Model: "local", JSONSchema: true}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { calls++; return nil, context.Canceled })})
	if _, err := c.Generate(context.Background(), ai.StoryRequest{Role: "writer", PromptVersion: "v1"}); err == nil || calls != 1 || !errors.Is(err, ai.ErrUnavailable) {
		t.Fatalf("transport error retried: %v", err)
	}
}
func TestRoleSchemasUseOnlyDocumentedSubsetAndNoSharedMaps(t *testing.T) {
	allowed := map[string]bool{"type": true, "properties": true, "required": true, "additionalProperties": true, "items": true, "minItems": true, "maxItems": true, "minimum": true, "maximum": true, "enum": true}
	var visit func(map[string]any)
	visit = func(s map[string]any) {
		for k := range s {
			if !allowed[k] {
				t.Fatalf("unsupported schema keyword %s", k)
			}
		}
		if p, ok := s["properties"].(map[string]any); ok {
			for _, v := range p {
				visit(v.(map[string]any))
			}
		}
		if v, ok := s["items"].(map[string]any); ok {
			visit(v)
		}
	}
	for _, role := range []string{"action_interpreter", "director", "pacing", "turn_planner", "writer", "choices", "world_evaluator", "state_evaluator", "quest_evaluator"} {
		s := roleSchema(role)
		if s == nil {
			t.Fatal(role)
		}
		visit(s)
		s["PRIVATE_MUTATION"] = true
		if _, exists := roleSchema(role)["PRIVATE_MUTATION"]; exists {
			t.Fatal("schema maps shared between requests")
		}
	}
	if roleSchema("lore_guard") != nil {
		t.Fatal("removed role was restored")
	}
}
