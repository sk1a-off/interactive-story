package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	browser "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/openai_compatible"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestProjectRequestsDistributedAcrossOneOrFourWorkers(t *testing.T) {
	for _, count := range []int{1, 4} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			b := newBridge("127.0.0.1:8791")
			b.maxActive = count
			for i := 0; i < count; i++ {
				b.workers[fmt.Sprintf("worker-%d", i)] = workerState{Seen: b.now(), Status: "ready", Version: "0.3.0"}
			}
			server := httptest.NewServer(b)
			defer server.Close()
			b.host = strings.TrimPrefix(server.URL, "http://")
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			client, err := browser.New(browser.Config{BaseURL: server.URL, Provider: "chatgpt_browser", Model: "gpt-6", APIKey: b.token, JSONSchema: true}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 8)
			for i := 0; i < 8; i++ {
				go func() {
					_, err := client.Generate(ctx, ai.StoryRequest{Role: "director", PromptVersion: "v1", Input: []byte(`{}`)})
					done <- err
				}()
			}
			claimed := make(chan string, count)
			release := make(chan struct{})
			var wg sync.WaitGroup
			for i := 0; i < count; i++ {
				wg.Add(1)
				go func(worker string) {
					defer wg.Done()
					first := true
					for ctx.Err() == nil {
						w := call(b, "/claim", map[string]string{"worker": worker, "version": "0.3.0"})
						if w.Code == 204 {
							time.Sleep(time.Millisecond)
							continue
						}
						if w.Code != 200 {
							t.Errorf("claim %d", w.Code)
							return
						}
						var j job
						_ = json.Unmarshal(w.Body.Bytes(), &j)
						if first {
							claimed <- j.ID
							first = false
							select {
							case <-release:
							case <-ctx.Done():
								return
							}
						}
						body := lease(j)
						if w := call(b, "/send", body); w.Code != 200 {
							t.Errorf("send %d", w.Code)
							return
						}
						body["reply"] = fmt.Sprintf(`{"goal":%q}`, j.ID)
						if w := call(b, "/result", body); w.Code != 200 {
							t.Errorf("result %d", w.Code)
							return
						}
					}
				}(fmt.Sprintf("worker-%d", i))
			}
			ids := map[string]bool{}
			for i := 0; i < count; i++ {
				select {
				case id := <-claimed:
					if ids[id] {
						t.Fatal("duplicate lease")
					}
					ids[id] = true
				case <-ctx.Done():
					t.Fatal("workers did not overlap", len(ids))
				}
			}
			if w := call(b, "/claim", map[string]string{"worker": "worker-extra", "version": "0.3.0"}); w.Code != 204 {
				t.Fatal("active limit bypassed", w.Code)
			}
			close(release)
			for i := 0; i < 8; i++ {
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-ctx.Done():
					t.Fatal("queue stalled")
				}
			}
			cancel()
			wg.Wait()
			b.mu.Lock()
			defer b.mu.Unlock()
			if len(b.jobs) != 8 {
				t.Fatal("missing/duplicate jobs", len(b.jobs))
			}
			for _, j := range b.jobs {
				if j.State != "complete" {
					t.Fatal("unfinished job", j.State)
				}
			}
		})
	}
}

func TestBrowserQueuePriorityAndExecutionBudgetStartOnClaim(t *testing.T) {
	b := newBridge("127.0.0.1:8791")
	now := time.Now()
	b.now = func() time.Time { return now }
	b.jobs = []*job{
		{ID: "background", Kind: "chat", Project: true, Background: true, ReasoningEffort: "none", State: "queued", Created: now, Deadline: now.Add(browser.BrowserQueueWait)},
		{ID: "foreground", Kind: "chat", Project: true, ReasoningEffort: "none", State: "queued", Created: now, Deadline: now.Add(browser.BrowserQueueWait)},
	}
	now = now.Add(8 * time.Minute)
	w := call(b, "/claim", map[string]string{"worker": "worker-one", "version": "0.3.0"})
	var j job
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &j) != nil || j.ID != "foreground" {
		t.Fatal("foreground did not get priority", w.Code, w.Body)
	}
	if !j.Deadline.Equal(now.Add(browser.BrowserTimeout("none"))) {
		t.Fatal("execution budget consumed by queue wait", j.Deadline)
	}
	if w := call(b, "/claim", map[string]string{"worker": "worker-one", "version": "0.3.0"}); w.Code != 409 {
		t.Fatal("same worker got two leases")
	}
	w = call(b, "/claim", map[string]string{"worker": "worker-two", "version": "0.3.0"})
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &j) != nil || j.ID != "background" {
		t.Fatal("idle second worker did not take background job", w.Code)
	}
}

func TestCancelledQueuedProjectIsNotLeasedToAnotherWorker(t *testing.T) {
	b := newBridge("127.0.0.1:8791")
	b.workers["worker-one"] = workerState{Seen: b.now(), Status: "ready", Version: "0.3.0"}
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("POST", "http://"+b.host+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-6","messages":[{"role":"user","content":"Synthetic cancellation"}]}`)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+b.token)
	r.Header.Set("X-Story-Project", "1")
	done := make(chan struct{})
	go func() { b.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	original := waitChatJob(t, b)
	cancel()
	<-done
	b.mu.Lock()
	if b.jobs[0].State != "failed" || b.jobs[0].Sent != nil {
		t.Fatal("cancelled queue entry remains active")
	}
	b.jobs = append(b.jobs, &job{ID: "other-job", State: "queued", Project: true, ReasoningEffort: "none", Deadline: b.now().Add(time.Minute)})
	b.mu.Unlock()
	w := call(b, "/claim", map[string]string{"worker": "worker-two", "version": "0.3.0"})
	var claimed job
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &claimed) != nil || claimed.ID == original.ID {
		t.Fatal("cancelled job leased again", w.Code, w.Body)
	}
}
