package openai_compatible

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestQuotaDelayExponentialJitterAndRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, header string
		retry        int
		jitter       float64
		want         time.Duration
		allowed      bool
	}{
		{"first floor", "", 0, 0, 500 * time.Millisecond, true},
		{"first ceiling", "", 0, 1, time.Second, true},
		{"second", "", 1, .5, 1500 * time.Millisecond, true},
		{"third", "", 2, .5, 3 * time.Second, true},
		{"cap", "", 99, 1, 8 * time.Second, true},
		{"seconds", "5", 0, 0, 5 * time.Second, true},
		{"date", now.Add(10 * time.Second).Format(http.TimeFormat), 0, 0, 10 * time.Second, true},
		{"past", now.Add(-time.Hour).Format(http.TimeFormat), 0, 0, 500 * time.Millisecond, true},
		{"negative", "-2", 0, 0, 500 * time.Millisecond, true},
		{"invalid", "invalid", 0, 0, 500 * time.Millisecond, true},
		{"long wait", "31", 0, 0, 31 * time.Second, false},
		{"overflow guard", "9223372036854775807", 0, 0, 24 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, allowed := quotaDelay(tc.retry, tc.header, now, tc.jitter)
			if got != tc.want || allowed != tc.allowed {
				t.Fatalf("delay=%s allowed=%v, want %s/%v", got, allowed, tc.want, tc.allowed)
			}
		})
	}
}

func immediateControl() *RequestControl {
	c := NewRequestControl(3, 1)
	c.backoff = func(int, string) (time.Duration, bool) { return 0, true }
	return c
}

func successfulHTTP(agent bool) *http.Response {
	body := `{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`
	if agent {
		body = `{"status":"completed","steps":[{"type":"model_output","content":[{"type":"text","text":"{\"ok\":true}"}]}]}`
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestQuotaRetriesAcrossProtocolsAreBoundedAndSanitized(t *testing.T) {
	for _, provider := range []string{"openai_compatible", "google_gemini"} {
		for _, mode := range []string{"chat", "agent", "stream", "agent-stream", "embedded"} {
			t.Run(provider+"/"+mode, func(t *testing.T) {
				if provider != "google_gemini" && (strings.HasPrefix(mode, "agent") || mode == "embedded") {
					t.Skip("Google-only wire protocol")
				}
				var calls int
				var keys []string
				model := "test"
				if strings.HasPrefix(mode, "agent") {
					model = "antigravity-preview-05-2026"
				}
				hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
					calls++
					keys = append(keys, r.Header.Get("Authorization")+r.Header.Get("x-goog-api-key"))
					status, body := 429, `{"error":"SECRET_PROVIDER_BODY"}`
					if mode == "embedded" {
						status, body = 200, `[{"error":{"code":429,"message":"SECRET_PROVIDER_BODY"}}]`
					}
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
				})}
				c, err := New(Config{BaseURL: "https://" + strings.ReplaceAll(t.Name(), "/", "-") + ".test/openai", Provider: provider, Model: model, APIKeys: []string{"SECRET_KEY_1", "SECRET_KEY_2"}, Requests: immediateControl()}, hc)
				if err != nil {
					t.Fatal(err)
				}
				req := writerRequest(nil)
				if strings.Contains(mode, "stream") {
					req.OnText = func(string) { t.Error("429 emitted text") }
				}
				_, err = c.Generate(context.Background(), req)
				if !errors.Is(err, ai.ErrUnavailable) || calls != 4 || strings.Contains(err.Error(), "SECRET") {
					t.Fatalf("calls=%d err=%v", calls, err)
				}
				if provider == "google_gemini" && keys[0] == keys[1] {
					t.Fatal("quota did not rotate credential")
				}
				if provider != "google_gemini" && keys[0] != keys[1] {
					t.Fatal("local endpoint unexpectedly rotated credential")
				}
			})
		}
	}
}

func waitPool(t *testing.T, c *RequestControl, scope string, active, high, low int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		p := c.pool(scope)
		ok := p.active == active && len(p.foreground) == high && len(p.background) == low
		c.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("request pool did not reach active=%d high=%d low=%d", active, high, low)
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("request failed to make progress")
		var zero T
		return zero
	}
}

func TestSharedClientsPrioritizeTurnOverEarlierImage(t *testing.T) {
	control := NewRequestControl(1, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan string, 3)
	releases := map[string]chan struct{}{"active": make(chan struct{}), "image": make(chan struct{}), "turn": make(chan struct{})}
	done := make(chan error, 3)
	start := func(name, role string) {
		c, _ := New(Config{BaseURL: "https://priority.test/openai", Provider: "google_gemini", Model: name, Requests: control}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			started <- name
			select {
			case <-releases[name]:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			return successfulHTTP(false), nil
		})})
		go func() {
			_, err := c.Generate(ctx, ai.StoryRequest{Role: role, PromptVersion: "v1", Input: []byte(`{}`)})
			done <- err
		}()
	}
	scope := requestScope("google_gemini", "https://priority.test/openai")
	start("active", "writer")
	if receive(t, started) != "active" {
		t.Fatal("initial request missing")
	}
	start("image", "image_prompt")
	waitPool(t, control, scope, 1, 0, 1)
	start("turn", "turn_planner")
	waitPool(t, control, scope, 1, 1, 1)
	close(releases["active"])
	if name := receive(t, started); name != "turn" {
		t.Fatalf("%s overtook the waiting player turn", name)
	}
	close(releases["turn"])
	if name := receive(t, started); name != "image" {
		t.Fatalf("illustration did not resume: %s", name)
	}
	close(releases["image"])
	for range 3 {
		if err := receive(t, done); err != nil {
			t.Fatal(err)
		}
	}
	waitPool(t, control, scope, 0, 0, 0)
}

func TestControlCancellationCooldownAndIndependentOrigins(t *testing.T) {
	c := NewRequestControl(1, 1)
	ctx, cancel := context.WithCancel(context.Background())
	release, err := c.acquire(context.Background(), "origin-one", true, false)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := c.acquire(ctx, "origin-one", true, false); done <- err }()
	waitPool(t, c, "origin-one", 1, 1, 0)
	cancel()
	if err := receive(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release()
	release() // an accidentally repeated close cannot create extra capacity
	waitPool(t, c, "origin-one", 0, 0, 0)
	c.cooldown("origin-one", time.Hour)
	deadline, stop := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer stop()
	if _, err := c.acquire(deadline, "origin-one", true, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cooldown ignored deadline: %v", err)
	}
	other, err := c.acquire(context.Background(), "origin-two", true, false)
	if err != nil {
		t.Fatal(err)
	}
	other()
	waitPool(t, c, "origin-one", 0, 0, 0)
}

func TestPhysicalConcurrencyCapsAcrossClientsAndModels(t *testing.T) {
	for _, provider := range []string{"google_gemini", "openai_compatible"} {
		t.Run(provider, func(t *testing.T) {
			control := NewRequestControl(3, 1)
			var active, peak atomic.Int32
			var wg sync.WaitGroup
			gate := make(chan struct{})
			entered := make(chan struct{}, 24)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			limit := 1
			if provider == "google_gemini" {
				limit = 3
			}
			for i := range 24 {
				c, _ := New(Config{BaseURL: "http://concurrency.test", Provider: provider, Model: fmt.Sprint(i), Requests: control}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
					n := active.Add(1)
					defer active.Add(-1)
					for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
					}
					entered <- struct{}{}
					select {
					case <-gate:
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
					return successfulHTTP(false), nil
				})})
				wg.Add(1)
				go func() {
					defer wg.Done()
					if _, err := c.Generate(ctx, writerRequest(nil)); err != nil {
						t.Error(err)
					}
				}()
			}
			waitPool(t, control, requestScope(provider, "http://concurrency.test"), limit, 24-limit, 0)
			for range limit {
				receive(t, entered)
			}
			close(gate)
			wg.Wait()
			if peak.Load() != int32(limit) {
				t.Fatalf("physical peak=%d, want %d", peak.Load(), limit)
			}
		})
	}
}

func TestStreamHoldsPermitUntilBodyClosed(t *testing.T) {
	control := NewRequestControl(1, 1)
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	var calls atomic.Int32
	c, _ := New(Config{BaseURL: "http://stream-cap.test", Model: "local", Requests: control}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}, nil
		}
		return successfulHTTP(false), nil
	})})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	first := make(chan string, 4)
	done := make(chan error, 2)
	go func() { _, err := c.Generate(ctx, writerRequest(func(v string) { first <- v })); done <- err }()
	go func() { _, _ = io.WriteString(writer, chatSSE(`{"text":"prefix`, nil)) }()
	if receive(t, first) != "prefix" {
		t.Fatal("missing prefix")
	}
	go func() { _, err := c.Generate(ctx, writerRequest(nil)); done <- err }()
	waitPool(t, control, requestScope("openai_compatible", "http://stream-cap.test"), 1, 1, 0)
	if calls.Load() != 1 {
		t.Fatal("stream released slot at headers")
	}
	_, _ = io.WriteString(writer, chatSSE(` end"}`, "stop")+"data: [DONE]\n\n")
	for range 2 {
		if err := receive(t, done); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("slot not released after terminal stream")
	}
}
