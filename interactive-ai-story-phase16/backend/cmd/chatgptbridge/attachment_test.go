package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTXTLeaseRequiresNewWorkerAndExactUploadAcknowledgement(t *testing.T) {
	b := newBridge("127.0.0.1:8791")
	b.projectInputFormat = "txt"
	request := func(ctx context.Context) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://"+b.host+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-6","messages":[{"role":"system","content":"Return JSON"},{"role":"user","content":"{\"action\":\"Проверить дверь\"}"}]}`)).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+b.token)
		r.Header.Set("X-Story-Project", "1")
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		return w
	}
	b.workers["worker-old"] = workerState{Version: "0.3.0", Status: "ready", Seen: b.now()}
	if w := request(context.Background()); w.Code != 503 || w.Header().Get("X-Story-Bridge-Worker-Version") != "0.4.0" {
		t.Fatal("file job allowed old worker", w.Code)
	}
	b.workers["worker-new"] = workerState{Version: "0.4.0", Status: "ready", Seen: b.now()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request(ctx) }()
	j := waitChatJob(t, b)
	if j.Attachment == nil || j.MessageFormat != attachmentFormat || len(j.Prompt) > 1024 || !strings.Contains(j.Attachment.Content, "Проверить дверь") {
		t.Fatal("wrong file envelope")
	}
	sum := sha256.Sum256([]byte(j.Attachment.Content))
	if hex.EncodeToString(sum[:]) != j.Attachment.SHA256 || j.Attachment.Bytes != len(j.Attachment.Content) {
		t.Fatal("file bytes changed")
	}
	if w := call(b, "/claim", map[string]string{"worker": "worker-old", "version": "0.3.0"}); w.Code != 204 {
		t.Fatal("old worker leased TXT")
	}
	w := call(b, "/claim", map[string]string{"worker": "worker-new", "version": "0.4.0"})
	var claimed job
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &claimed) != nil {
		t.Fatal("new worker unable to claim", w.Code)
	}
	body := lease(claimed)
	if w := call(b, "/send", body); w.Code != 409 {
		t.Fatal("unverified upload sent")
	}
	ack := map[string]any{"worker": claimed.Worker, "id": claimed.ID, "lease": claimed.Lease, "fileName": claimed.Attachment.Name, "fileSha256": "wrong", "fileBytes": claimed.Attachment.Bytes}
	if w := call(b, "/attachment", ack); w.Code != 409 {
		t.Fatal("wrong hash accepted")
	}
	ack["fileSha256"] = claimed.Attachment.SHA256
	if w := call(b, "/attachment", ack); w.Code != 200 {
		t.Fatal("valid upload not verified", w.Code)
	}
	if w := call(b, "/send", body); w.Code != 200 {
		t.Fatal(w.Code)
	}
	body["reply"] = `{"goal":"Проверить дверь"}`
	if w := call(b, "/result", body); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := <-done; w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestProjectTransportLimitAndDiagnostics(t *testing.T) {
	b := newBridge("127.0.0.1:8791")
	b.maxPromptBytes = 128 << 10
	b.projectInputFormat = "txt"
	data, _ := json.Marshal(map[string]any{"model": "gpt-6", "messages": []any{map[string]string{"role": "user", "content": strings.Repeat("Данные ", 20000)}}})
	r := httptest.NewRequest("POST", "http://"+b.host+"/v1/chat/completions", strings.NewReader(string(data)))
	r.Header.Set("Authorization", "Bearer "+b.token)
	r.Header.Set("X-Story-Project", "1")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 413 || w.Header().Get("X-Story-Bridge-Limit-Bytes") != "131072" || w.Header().Get("X-Story-Bridge-Limit-Scope") != "prompt" || len(b.jobs) != 0 {
		t.Fatal("oversized file not bounded", w.Code, w.Header())
	}
	for _, raw := range []string{"0", "127", "4097", "abc"} {
		if _, err := projectPromptLimit(raw); err == nil {
			t.Fatal("invalid limit accepted", raw)
		}
	}
	if got, err := projectPromptLimit(""); err != nil || got != 1<<20 {
		t.Fatal(got, err)
	}
}

func TestTXTContentPreservesLargeNumbersAndLiteralUnicode(t *testing.T) {
	var in completionRequest
	if err := json.Unmarshal([]byte(`{"messages":[{"role":"system","content":"Системные инструкции"},{"role":"user","content":"{\"integer\":9007199254740993,\"text\":\"Лада\\nвторая строка\"}"}]}`), &in); err != nil {
		t.Fatal(err)
	}
	prompt, file := textAttachment(in, "synthetic")
	if !strings.Contains(file.Content, "9007199254740993") || !strings.Contains(file.Content, "Лада") || !strings.Contains(file.Content, `\nвторая строка`) || len(prompt) > 1024 {
		t.Fatal("TXT changed data")
	}
}
