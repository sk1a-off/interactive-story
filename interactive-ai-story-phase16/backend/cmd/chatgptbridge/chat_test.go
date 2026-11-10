package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func waitChatJob(t *testing.T, b *bridge) job {
	t.Helper()
	for i := 0; i < 200; i++ {
		b.mu.Lock()
		if len(b.jobs) > 0 {
			v := *b.jobs[0]
			b.mu.Unlock()
			return v
		}
		b.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job not queued")
	return job{}
}
func TestChatCompletionThroughLease(t *testing.T) {
	b := newBridge("127.0.0.1:8791")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- call(b, "/v1/chat/completions", completionRequest{Model: "gpt-6-instant", Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{{"system", "Return JSON"}, {"user", "Synthetic story"}}})
	}()
	j := waitChatJob(t, b)
	if j.Kind != "chat" || !strings.Contains(j.Prompt, "Synthetic story") {
		t.Fatal(j)
	}
	c := claim(t, b, "worker-one")
	body := lease(c)
	call(b, "/send", body)
	body["reply"] = "```json\n{\"text\":\"Длинный ответ\"}\n```"
	call(b, "/result", body)
	w := <-done
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Choices) != 1 || out.Choices[0].Message.Content != `{"text":"Длинный ответ"}` {
		t.Fatal(w.Body)
	}
	if w = call(b, "/result", body); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"complete"`) {
		t.Fatal("idempotent reply lacks state")
	}
}
func TestChatCancellationDoesNotRequeue(t *testing.T) {
	b := newBridge("127.0.0.1:8791")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("POST", "http://"+b.host+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-6-instant","messages":[{"role":"user","content":"synthetic"}]}`)).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+b.token)
	done := make(chan struct{})
	go func() { b.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	waitChatJob(t, b)
	j := claim(t, b, "worker-one")
	call(b, "/send", lease(j))
	cancel()
	<-done
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.jobs[0].State != "uncertain" {
		t.Fatal("cancelled send must remain uncertain")
	}
}
func TestChatRejectsUnsupportedProtocol(t *testing.T) {
	b := newBridge("127.0.0.1:8791")
	for _, body := range []any{map[string]any{"model": "other", "messages": []any{}}, map[string]any{"model": "gpt-6-instant", "stream": true}, map[string]any{"model": "gpt-6-instant", "messages": []any{map[string]string{"role": "tool", "content": "x"}}}} {
		if w := call(b, "/v1/chat/completions", body); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	for _, raw := range []string{`[]`, `null`, `prefix {"text":"a"}`, `{} {}`} {
		if _, ok := jsonReply(raw); ok {
			t.Fatal("invalid object accepted", raw)
		}
	}
}
func TestLocalTokenFilePreservesToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret", "token")
	token := secret()
	got, err := tokenFile(path, token)
	if err != nil || got != token {
		t.Fatal(err)
	}
	got, err = tokenFile(path, secret())
	if err != nil || got != token {
		t.Fatal("token rotated", err)
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = tokenFile(path, secret()); err == nil {
		t.Fatal("readable secret file accepted")
	}
}

func TestRenderedEnvelopePreservesJSONWithoutMarkdown(t *testing.T) {
	original := map[string]any{"messages": []map[string]string{{"role": "system", "content": "# Title\nUse `goal` and **bold**, [link](https://example.test), a_b, C:\\archive and \"quotes\".\u2028Лада"}}, "response_format": map[string]string{"type": "json_object"}}
	raw, _ := json.Marshal(original)
	safe, err := renderSafeJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if json.Unmarshal([]byte(safe), &decoded) != nil {
		t.Fatal("not JSON")
	}
	normalized, _ := json.Marshal(decoded)
	if string(normalized) != string(raw) {
		t.Fatal("transport changed logical messages")
	}
	if strings.ContainsAny(safe, "`*_#~()") || strings.Contains(safe, "\n") {
		t.Fatal("rendered Markdown syntax leaked", safe)
	}
}
