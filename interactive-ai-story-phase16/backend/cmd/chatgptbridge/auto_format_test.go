package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAutoFormatRoutesOnlyTextOverLimitToTXT(t *testing.T) {
	for _, tc := range []struct {
		name       string
		characters int
		wantFile   bool
	}{
		{"below", defaultTextCharacters - 1, false},
		{"at limit", defaultTextCharacters, false},
		{"one over", defaultTextCharacters + 1, true},
		{"reported size", 69413, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBridge("127.0.0.1:8791")
			b.projectInputFormat = "auto"
			const system = "Выбери следующий момент для иллюстрации."
			const user = "Начало сцены 😀. Конец сцены."
			in := completionRequest{ResponseFormat: json.RawMessage(`{"type":"json_object"}`)}
			if err := json.Unmarshal([]byte(`{"model":"gpt-6","messages":[{"role":"system"},{"role":"user"}]}`), &in); err != nil {
				t.Fatal(err)
			}
			in.Messages[0].Content = system
			in.Messages[1].Content = user
			base, _, _ := readableMessage(in, strings.Repeat("0", len(secret())))
			story := strings.Replace(user, "Конец", strings.Repeat("А", tc.characters-utf16Characters(base))+"Конец", 1)
			in.Messages[1].Content = story
			b.workers["worker-auto"] = workerState{Version: chatGPTWorkerVersion, Status: "ready", Seen: b.now()}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			data, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "http://"+b.host+"/v1/chat/completions", strings.NewReader(string(data))).WithContext(ctx)
			r.Header.Set("Authorization", "Bearer "+b.token)
			r.Header.Set("X-Story-Project", "1")
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { w := httptest.NewRecorder(); b.ServeHTTP(w, r); done <- w }()
			j := waitChatJob(t, b)
			plain, _, _ := readableMessage(in, j.ID)
			if got := utf16Characters(plain); got != tc.characters {
				t.Fatalf("test prompt length = %d, want %d", got, tc.characters)
			}
			if j.TextOnly == tc.wantFile || (j.Attachment != nil) != tc.wantFile {
				t.Fatal("queued the wrong transport")
			}
			if tc.wantFile {
				if len(j.Prompt) > 1024 || j.MessageFormat != attachmentFormat || !strings.Contains(j.Attachment.Content, story) || !strings.Contains(j.Attachment.Content, system) || !strings.Contains(j.Attachment.Content, string(in.ResponseFormat)) {
					t.Fatal("TXT did not receive the complete request")
				}
			} else if j.Prompt != plain {
				t.Fatal("inline text changed")
			}
			cancel()
			<-done
		})
	}
}
