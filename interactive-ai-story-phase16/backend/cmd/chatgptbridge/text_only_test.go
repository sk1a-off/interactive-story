package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTextOnlyRequiresCompatibleWorkerAndNoAttachmentProof(t *testing.T) {
	b := newBridge("127.0.0.1:8791")
	b.projectInputFormat = "text"
	request := func(ctx context.Context) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://"+b.host+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-6","messages":[{"role":"system","content":"Return JSON"},{"role":"user","content":"Проверить текст"}]}`)).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+b.token)
		r.Header.Set("X-Story-Project", "1")
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		return w
	}
	b.workers["worker-old"] = workerState{Version: "0.4.0", Status: "ready", Seen: b.now()}
	if w := request(context.Background()); w.Code != 503 || w.Header().Get("X-Story-Bridge-Worker-Version") != "0.4.1" {
		t.Fatal("old worker allowed text-only job", w.Code)
	}
	b.workers["worker-new"] = workerState{Version: "0.4.1", Status: "ready", Seen: b.now()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request(ctx) }()
	j := waitChatJob(t, b)
	if !j.TextOnly || j.Attachment != nil || j.MessageFormat != "readable-json-v3" || !strings.Contains(j.Prompt, "Проверить текст") {
		t.Fatal("wrong text envelope")
	}
	if w := call(b, "/claim", map[string]string{"worker": "worker-old", "version": "0.4.0"}); w.Code != 204 {
		t.Fatal("old worker claimed strict text")
	}
	w := call(b, "/claim", map[string]string{"worker": "worker-new", "version": "0.4.1"})
	var leased job
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &leased) != nil {
		t.Fatal(w.Code)
	}
	body := map[string]any{"worker": leased.Worker, "id": leased.ID, "lease": leased.Lease, "version": "0.4.1"}
	if w := call(b, "/send", body); w.Code != 409 {
		t.Fatal("unverified text authorized", w.Code)
	}
	body["textOnlyVerified"] = true
	if w := call(b, "/send", body); w.Code != 200 {
		t.Fatal(w.Code)
	}
	body["reply"] = `{"goal":"Проверить текст"}`
	if w := call(b, "/result", body); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := <-done; w.Code != 200 {
		t.Fatal(w.Code)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.jobs[0].TextOnlyVerified || b.jobs[0].AttachmentVerified {
		t.Fatal("wrong verification record")
	}
}

func TestTextSafetyLimitRejectsBeforeQueuingWithoutTruncation(t *testing.T) {
	b := newBridge("127.0.0.1:8791")
	b.projectInputFormat = "text"
	b.maxTextCharacters = 1024
	data, _ := json.Marshal(map[string]any{"model": "gpt-6", "messages": []any{map[string]string{"role": "user", "content": strings.Repeat("Текст ", 300)}}})
	r := httptest.NewRequest("POST", "http://"+b.host+"/v1/chat/completions", strings.NewReader(string(data)))
	r.Header.Set("Authorization", "Bearer "+b.token)
	r.Header.Set("X-Story-Project", "1")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 413 || w.Header().Get("X-Story-Bridge-Limit-Scope") != "text_characters" || len(b.jobs) != 0 {
		t.Fatal("oversized text queued or silently changed", w.Code)
	}
	if utf16Characters("Лада😀") != 6 {
		t.Fatal("UTF-16 composer length mismatch")
	}
	for _, raw := range []string{"0", "1023", "1048577", "abc"} {
		if _, err := textCharacterLimit(raw); err == nil {
			t.Fatal("invalid text limit", raw)
		}
	}
}
