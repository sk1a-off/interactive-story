package narration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceUsesExistingVoicePromptContract(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["input"] != "Новый абзац истории." || body["voice"] != "russian-reference" || body["response_format"] != "wav" {
			t.Fatalf("unexpected request: %#v", body)
		}
		if _, exists := body["reference_audio"]; exists {
			t.Fatal("proxy must not request voice creation")
		}
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("RIFF-audio"))
	}))
	defer server.Close()

	service, err := NewService(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	audio, contentType, err := service.Speech(context.Background(), "Новый абзац истории.")
	if err != nil {
		t.Fatal(err)
	}
	if string(audio) != "RIFF-audio" || contentType != "audio/wav" {
		t.Fatalf("audio=%q contentType=%q", audio, contentType)
	}
}

func TestServiceRejectsOversizedTextBeforeCallingUpstream(t *testing.T) {
	t.Parallel()
	service, err := NewService("http://127.0.0.1:6655", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = service.Speech(context.Background(), strings.Repeat("я", MaxTextChars+1))
	if err != ErrInvalidText {
		t.Fatalf("error = %v", err)
	}
}
