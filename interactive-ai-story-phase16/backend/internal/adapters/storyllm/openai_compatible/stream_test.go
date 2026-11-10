package openai_compatible

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type aiRequest = ai.StoryRequest

func TestWriterTextStreamEveryByteBoundaryAndPrivateFields(t *testing.T) {
	raw := `{"private":{"text":"DO_NOT_SHOW"},"text":"Привет \ud83d\ude00 \"\\\/\nконец","other":"HIDDEN"}`
	var want struct{ Text string }
	if json.Unmarshal([]byte(raw), &want) != nil {
		t.Fatal("bad test fixture")
	}
	p := writerTextStream{}
	var snapshots []string
	for i := 0; i < len(raw); i++ {
		if err := p.feed(raw[i:i+1], func(text string) { snapshots = append(snapshots, text) }); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.finish(func(text string) { snapshots = append(snapshots, text) }); err != nil {
		t.Fatal(err)
	}
	if len(snapshots) < 3 || snapshots[len(snapshots)-1] != want.Text {
		t.Fatalf("missing decoded stream: %v", snapshots)
	}
	for i, text := range snapshots {
		if strings.Contains(text, "DO_NOT_SHOW") || strings.Contains(text, "HIDDEN") || strings.Contains(text, "�") {
			t.Fatal("private or invalid text leaked")
		}
		if i > 0 && !strings.HasPrefix(text, snapshots[i-1]) {
			t.Fatal("snapshots stopped being cumulative")
		}
	}
}
func TestWriterTextStreamRejectsMalformedAndDuplicateOutput(t *testing.T) {
	for _, raw := range []string{`{"text":"unfinished`, `{"text":null}`, `{"text":"one","text":"two"}`, `{"text":"bad\x"}`, `{"text":"\ud800"}`, `{"text":"ok"} trailing`} {
		p := writerTextStream{}
		err := p.feed(raw, nil)
		if err == nil {
			_, err = p.finish(nil)
		}
		if err == nil {
			t.Fatalf("invalid stream accepted: %s", raw)
		}
	}
	p := writerTextStream{}
	if err := p.feed(strings.Repeat("x", maxWriterJSON+1), nil); err == nil {
		t.Fatal("stream limit missing")
	}
}
func chatSSE(fragment string, finish any) string {
	body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": fragment}, "finish_reason": finish}}})
	return "data: " + string(body) + "\n\n"
}
func TestChatWriterStreamsBeforeCompletionAndRetainsUsage(t *testing.T) {
	reader, writer := io.Pipe()
	release := make(chan struct{})
	first := make(chan string, 8)
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		var body chatRequest
		_ = json.Unmarshal(raw, &body)
		if !body.Stream || !body.StreamOptions["include_usage"] {
			t.Error("stream request missing")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}, nil
	})}
	c, _ := New(Config{BaseURL: "http://stream.test", Model: "local"}, hc)
	done := make(chan error, 1)
	go func() {
		response, err := c.Generate(context.Background(), writerRequest(func(s string) { first <- s }))
		if err == nil && (!response.Streamed || response.InputTokens != 10 || response.OutputTokens != 4 || response.CachedInputTokens == nil || *response.CachedInputTokens != 0) {
			err = fmt.Errorf("lost stream metadata")
		}
		done <- err
	}()
	go func() {
		_, _ = io.WriteString(writer, chatSSE(`{"text":"Начало`, nil))
		<-release
		_, _ = io.WriteString(writer, chatSSE(` и конец"}`, "stop")+`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":0}}}`+"\n\ndata: [DONE]\n\n")
		_ = writer.Close()
	}()
	select {
	case text := <-first:
		if text != "Начало" {
			t.Fatal(text)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("no prefix before completion")
	}
	select {
	case err := <-done:
		close(release)
		t.Fatalf("finished before provider completed: %v", err)
	default:
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not finish")
	}
}
func writerRequest(cb func(string)) aiRequest {
	return aiRequest{Role: "writer", PromptVersion: "v1", Input: []byte(`{}`), OnText: cb}
}
func TestAgentStreamIgnoresThoughtAndToolText(t *testing.T) {
	events := []string{
		`{"event_type":"step.start","index":0,"step":{"type":"thought"}}`,
		`{"event_type":"step.delta","index":0,"delta":{"type":"text","text":"{\"text\":\"SECRET_THOUGHT\"}"}}`,
		`{"event_type":"step.start","index":1,"step":{"type":"tool_result"}}`,
		`{"event_type":"step.delta","index":1,"delta":{"type":"text","text":"SECRET_TOOL"}}`,
		`{"event_type":"step.start","index":2,"step":{"type":"model_output"}}`,
		`{"event_type":"step.delta","index":2,"delta":{"type":"text","text":"{\"text\":\"Рассказ"}}`,
		`{"event_type":"step.delta","index":2,"delta":{"type":"text","text":" продолжается\"}"}}`,
		`{"event_type":"interaction.completed","interaction":{"status":"completed","usage":{"total_input_tokens":20,"total_output_tokens":5,"total_cached_tokens":0}}}`,
	}
	body := ""
	for _, event := range events {
		body += "data: " + event + "\n\n"
	}
	c, _ := New(Config{BaseURL: "https://agent-stream.test/openai", Provider: "google_gemini", Model: "antigravity-preview-05-2026"}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	var snapshots []string
	response, err := c.Generate(context.Background(), writerRequest(func(s string) { snapshots = append(snapshots, s) }))
	if err != nil || !response.Streamed || response.InputTokens != 20 || len(snapshots) < 2 {
		t.Fatalf("agent stream failed: %v %v", err, snapshots)
	}
	for _, s := range snapshots {
		if strings.Contains(s, "SECRET") {
			t.Fatal("agent private data exposed")
		}
	}
}
func TestStreamTruncationDoesNotSilentlyRetryAfterText(t *testing.T) {
	for _, body := range []string{chatSSE(`{"text":"prefix`, nil), chatSSE(`{"text":"complete"}`, "stop"), chatSSE(`{"text":"prefix`, nil) + "data: {\"error\":{\"code\":503}}\n\n"} {
		calls := 0
		c, _ := New(Config{BaseURL: "http://broken-stream.test", Model: "local"}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})})
		_, err := c.Generate(context.Background(), writerRequest(func(string) {}))
		if err == nil || calls != 1 {
			t.Fatal("partial stream retried or accepted")
		}
	}
}
func TestExplicitStreamRejectionUsesOneFullFallbackAndCaches(t *testing.T) {
	calls := 0
	c, _ := New(Config{BaseURL: "http://unsupported-stream.test", Model: "local"}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return schemaHTTP(400, `{"error":"stream is unsupported"}`), nil
		}
		return schemaHTTP(200, `{"choices":[{"message":{"content":"{\"text\":\"Готово\"}"}}]}`), nil
	})})
	for i := 0; i < 2; i++ {
		response, err := c.Generate(context.Background(), writerRequest(func(string) {}))
		if err != nil || i == 0 && (!response.StreamFallback || response.ProtocolRetries != 1) {
			t.Fatal("bounded fallback failed")
		}
	}
	if calls != 3 {
		t.Fatalf("stream rejection repeatedly probed: %d", calls)
	}
}

func TestStreamingCancellationStopsProviderRequestWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, chatSSE(`{"text":"Начало`, nil))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	c, _ := New(Config{BaseURL: server.URL, Model: "local"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := c.Generate(ctx, writerRequest(func(string) {
			select {
			case first <- struct{}{}:
			default:
			}
		}))
		done <- err
	}()
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("no first text")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lost cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not cancel")
	}
	if calls.Load() != 1 {
		t.Fatal("cancelled stream retried")
	}
}
func TestStreamSchemaFallbackRemainsStreamingAndBounded(t *testing.T) {
	calls := 0
	c, _ := New(Config{BaseURL: "http://schema-stream.test", Model: "local", JSONSchema: true}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		var payload chatRequest
		_ = json.Unmarshal(raw, &payload)
		if !payload.Stream {
			t.Fatal("schema fallback disabled streaming")
		}
		if calls == 1 {
			return schemaHTTP(400, `{"error":"schema unsupported"}`), nil
		}
		if payload.ResponseFormat.Type != "json_object" {
			t.Fatal("schema fallback missing")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(chatSSE(`{"text":"Готово"}`, "stop") + "data: [DONE]\n\n"))}, nil
	})})
	response, err := c.Generate(context.Background(), writerRequest(func(string) {}))
	if err != nil || calls != 2 || !response.Streamed || !response.SchemaFallback || response.ProtocolRetries != 1 {
		t.Fatal("combined schema streaming contract failed")
	}
}
