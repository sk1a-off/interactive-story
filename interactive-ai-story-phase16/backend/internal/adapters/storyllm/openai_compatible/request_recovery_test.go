package openai_compatible

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestQuotaCooldownIsSharedAndTurnOvertakesImageRetry(t *testing.T) {
	control := NewRequestControl(1, 1)
	const delay = 60 * time.Millisecond
	control.backoff = func(int, string) (time.Duration, bool) { return delay, true }
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var calls atomic.Int32
	order := make(chan string, 3)
	firstAt := make(chan time.Time, 1)
	makeClient := func(name string) *Client {
		c, err := New(Config{BaseURL: "https://shared-backoff.test/openai", Provider: "google_gemini", Model: name, Requests: control}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				firstAt <- time.Now()
				return schemaHTTP(429, `{"error":"quota"}`), nil
			}
			order <- name
			return successfulHTTP(false), nil
		})})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	image, turn := makeClient("image"), makeClient("turn")
	done := make(chan error, 2)
	go func() {
		_, err := image.Generate(ctx, ai.StoryRequest{Role: "image_prompt", PromptVersion: "v1"})
		done <- err
	}()
	started := receive(t, firstAt)
	scope := requestScope("google_gemini", "https://shared-backoff.test/openai")
	waitPool(t, control, scope, 0, 0, 1) // backoff occupies no HTTP slot
	go func() { _, err := turn.Generate(ctx, writerRequest(nil)); done <- err }()
	waitPool(t, control, scope, 0, 1, 1)
	if got := receive(t, order); got != "turn" {
		t.Fatalf("image quota retry overtook turn: %s", got)
	}
	if elapsed := time.Since(started); elapsed < delay {
		t.Fatalf("different model bypassed shared cooldown: %s", elapsed)
	}
	if got := receive(t, order); got != "image" {
		t.Fatal("image retry did not recover")
	}
	for range 2 {
		if err := receive(t, done); err != nil {
			t.Fatal(err)
		}
	}
}

func TestQuotaCancellationDoesNotIssueAnotherPost(t *testing.T) {
	control := NewRequestControl(1, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	c, _ := New(Config{BaseURL: "http://cancel-quota.test", Model: "local", Requests: control}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		r := schemaHTTP(429, `{"error":"quota"}`)
		r.Header.Set("Retry-After", "30")
		return r, nil
	})})
	done := make(chan error, 1)
	go func() { _, err := c.Generate(ctx, writerRequest(nil)); done <- err }()
	scope := requestScope("openai_compatible", "http://cancel-quota.test")
	waitPool(t, control, scope, 0, 1, 0)
	cancel()
	if err := receive(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("cancellation spent another POST")
	}
	waitPool(t, control, scope, 0, 0, 0)
}

func TestQuotaBudgetSurvivesProtocolFallback(t *testing.T) {
	for _, fallback := range []string{"schema", "stream"} {
		t.Run(fallback, func(t *testing.T) {
			control := immediateControl()
			calls := 0
			c, _ := New(Config{BaseURL: "http://fallback-budget.test", Model: "local", Requests: control, JSONSchema: true}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 2 {
					if fallback == "stream" {
						return schemaHTTP(400, `{"error":"stream unsupported"}`), nil
					}
					return schemaHTTP(400, `{"error":"response_format json_schema unsupported"}`), nil
				}
				return schemaHTTP(429, `{"error":"quota"}`), nil
			})})
			req := writerRequest(nil)
			if fallback == "stream" {
				req.OnText = func(string) { t.Error("failed request emitted text") }
			}
			_, err := c.Generate(context.Background(), req)
			if !errors.Is(err, ai.ErrUnavailable) || calls != 5 {
				t.Fatalf("protocol fallback reset quota budget: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestQuotaCanRecoverBeforeStreamAndNeverRetriesPartialStream(t *testing.T) {
	for _, truncate := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "truncated"}[truncate], func(t *testing.T) {
			calls := 0
			var snapshots []string
			c, _ := New(Config{BaseURL: "http://quota-stream.test", Model: "local", Requests: immediateControl()}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return schemaHTTP(429, `{"error":"quota"}`), nil
				}
				body := chatSSE(`{"text":"prefix`, nil)
				if !truncate {
					body += chatSSE(` end"}`, "stop") + "data: [DONE]\n\n"
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})})
			_, err := c.Generate(context.Background(), writerRequest(func(v string) { snapshots = append(snapshots, v) }))
			if calls != 2 || len(snapshots) == 0 {
				t.Fatalf("calls=%d snapshots=%d", calls, len(snapshots))
			}
			if truncate && !errors.Is(err, ai.ErrInvalidOutput) || !truncate && err != nil {
				t.Fatalf("unexpected stream result: %v", err)
			}
			waitPool(t, c.cfg.Requests, requestScope("openai_compatible", c.cfg.BaseURL), 0, 0, 0)
		})
	}
}

func TestRejectedKeysSkipWithoutBackoffButDoNotLoop(t *testing.T) {
	calls, waits := 0, 0
	control := immediateControl()
	control.backoff = func(int, string) (time.Duration, bool) { waits++; return 0, true }
	c, _ := New(Config{BaseURL: "https://bad-keys.test/openai", Provider: "google_gemini", Model: "test", APIKeys: []string{"one", "two", "three"}, Requests: control}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return schemaHTTP(403, `{"error":"api key rejected SECRET"}`), nil
	})})
	_, err := c.Generate(context.Background(), writerRequest(nil))
	if !errors.Is(err, ai.ErrUnavailable) || calls != 3 || waits != 0 || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("calls=%d waits=%d err=%v", calls, waits, err)
	}
}

func TestHTTPQuotaRetryHonorsRetryAfterAndKeepsBody(t *testing.T) {
	var calls atomic.Int32
	var original string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if calls.Add(1) == 1 {
			original = string(raw)
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"error":"quota"}`)
			return
		}
		if string(raw) != original {
			t.Error("quota retry changed the generation payload")
		}
		response := successfulHTTP(false)
		defer response.Body.Close()
		_, _ = io.Copy(w, response.Body)
	}))
	defer server.Close()
	c, _ := New(Config{BaseURL: server.URL, Model: "local"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	_, err := c.Generate(ctx, writerRequest(nil))
	if err != nil || calls.Load() != 2 || time.Since(started) < time.Second {
		t.Fatalf("calls=%d elapsed=%s err=%v", calls.Load(), time.Since(started), err)
	}
}

type brokenResponseBody struct{ io.ReadCloser }

func (brokenResponseBody) Read([]byte) (int, error) { return 0, errors.New("SECRET body error") }

func TestFailedTransportOrBodyReleasesPermitAndSanitizesErrors(t *testing.T) {
	for _, mode := range []string{"transport", "body"} {
		t.Run(mode, func(t *testing.T) {
			control := NewRequestControl(1, 1)
			calls := 0
			c, _ := New(Config{BaseURL: "http://failed-body.test", Model: "local", Requests: control}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					if mode == "transport" {
						return nil, errors.New("SECRET transport error")
					}
					return &http.Response{StatusCode: 200, Body: brokenResponseBody{io.NopCloser(strings.NewReader(""))}}, nil
				}
				return successfulHTTP(false), nil
			})})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := c.Generate(ctx, writerRequest(nil))
			if !errors.Is(err, ai.ErrUnavailable) || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("unexpected or unsanitized error: %v", err)
			}
			if _, err := c.Generate(ctx, writerRequest(nil)); err != nil || calls != 2 {
				t.Fatalf("failure leaked slot: calls=%d err=%v", calls, err)
			}
		})
	}
}
