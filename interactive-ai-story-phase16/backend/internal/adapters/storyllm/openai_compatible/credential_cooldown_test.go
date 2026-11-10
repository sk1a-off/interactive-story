package openai_compatible

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestLongRetryAfterRotatesAvailableKeyWithoutBlockingOtherModels(t *testing.T) {
	control := immediateControl()
	var auth []string
	base := "https://long-key-hint.test/openai"
	c, _ := New(Config{BaseURL: base, Provider: "google_gemini", Model: "limited-model", APIKeys: []string{"limited-key", "ready-key"}, Requests: control}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		auth = append(auth, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") == "Bearer limited-key" {
			response := schemaHTTP(429, `{"error":"quota SECRET"}`)
			response.Header.Set("Retry-After", "3600")
			return response, nil
		}
		return successfulHTTP(false), nil
	})})
	rememberKeyCursor(c.cfg.Provider+"\x00"+c.cfg.Model+"\x00"+base, 0, 2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.Generate(ctx, writerRequest(nil)); err != nil || len(auth) != 2 || auth[1] != "Bearer ready-key" {
		t.Fatalf("long hint blocked a usable key: calls=%d err=%v", len(auth), err)
	}
	// Same key/origin but a different model has no observed long quota hint.
	other, _ := New(Config{BaseURL: base, Provider: "google_gemini", Model: "other-model", APIKeys: []string{"limited-key"}, Requests: control}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return successfulHTTP(false), nil })})
	if _, err := other.Generate(ctx, writerRequest(nil)); err != nil {
		t.Fatalf("one model's hint blocked a different model: %v", err)
	}
	// The rejected credential remains unavailable across client instances.
	limited, _ := New(Config{BaseURL: base, Provider: "google_gemini", Model: "limited-model", APIKeys: []string{"limited-key"}, Requests: control}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		t.Error("cooling credential was sent again")
		return successfulHTTP(false), nil
	})})
	if _, err := limited.Generate(ctx, writerRequest(nil)); !errors.Is(err, ai.ErrUnavailable) || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("expected safe unavailable error: %v", err)
	}
}

func TestAllLongCoolingCredentialsFailWithoutSleepingOrMorePosts(t *testing.T) {
	control := immediateControl()
	posts := 0
	c, _ := New(Config{BaseURL: "https://all-cooling.test/openai", Provider: "google_gemini", Model: "limited", APIKeys: []string{"one", "two"}, Requests: control}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		posts++
		response := schemaHTTP(429, `{"error":"quota"}`)
		response.Header.Set("Retry-After", "86400")
		return response, nil
	})})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for range 2 {
		if _, err := c.Generate(ctx, writerRequest(nil)); !errors.Is(err, ai.ErrUnavailable) {
			t.Fatalf("all-cooling request did not fail promptly: %v", err)
		}
	}
	if posts != 2 {
		t.Fatalf("cooling keys consumed additional quota: %d posts", posts)
	}
	waitPool(t, control, requestScope(c.cfg.Provider, c.cfg.BaseURL), 0, 0, 0)
}

func TestLocalLongCooldownDoesNotEnableCredentialRotation(t *testing.T) {
	posts := 0
	c, _ := New(Config{BaseURL: "https://local-cooling.test", Model: "local", APIKeys: []string{"first", "second"}, Requests: immediateControl()}, &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		posts++
		if r.Header.Get("Authorization") != "Bearer first" {
			t.Error("local provider changed identity")
		}
		response := schemaHTTP(429, `{"error":"quota"}`)
		response.Header.Set("Retry-After", "3600")
		return response, nil
	})})
	if _, err := c.Generate(context.Background(), writerRequest(nil)); !errors.Is(err, ai.ErrUnavailable) || posts != 1 {
		t.Fatalf("local identity changed or quota ignored: calls=%d err=%v", posts, err)
	}
}
