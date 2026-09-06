package role_router

import (
	"context"
	"errors"
	"testing"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

type recordingLLM struct {
	identity aiport.ProviderIdentity
	roles    []string
	err      error
}

func (r *recordingLLM) Identity() aiport.ProviderIdentity { return r.identity }
func (r *recordingLLM) Generate(_ context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	r.roles = append(r.roles, request.Role)
	if r.err != nil {
		return aiport.StoryResponse{}, r.err
	}
	return aiport.StoryResponse{Output: []byte(`{"ok":true}`)}, nil
}

func TestSafeHybridRoutesOnlyNonCanonicalPreparationToFastModel(t *testing.T) {
	fast := &recordingLLM{identity: aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "google_gemini", Model: "gemini-3.5-flash-lite"}}
	quality := &recordingLLM{identity: aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "google_gemini", Model: "antigravity-preview-05-2026"}}
	router, err := NewSafeHybrid(fast, quality)
	if err != nil {
		t.Fatal(err)
	}

	fastRoles := []string{"action_interpreter", "pacing", "choices"}
	qualityRoles := []string{"director", "writer", "world_evaluator", "state_evaluator", "quest_evaluator", "structured_repair", "setup_architect", "image_prompt"}
	for _, role := range append(append([]string{}, fastRoles...), qualityRoles...) {
		if _, err = router.Generate(context.Background(), aiport.StoryRequest{Role: role}); err != nil {
			t.Fatal(err)
		}
	}
	if len(fast.roles) != len(fastRoles) {
		t.Fatalf("fast model roles = %v", fast.roles)
	}
	for index, role := range fastRoles {
		if fast.roles[index] != role {
			t.Fatalf("fast role %d = %q, want %q", index, fast.roles[index], role)
		}
	}
	if len(quality.roles) != len(qualityRoles) {
		t.Fatalf("quality model roles = %v", quality.roles)
	}
	for index, role := range qualityRoles {
		if quality.roles[index] != role {
			t.Fatalf("quality role %d = %q, want %q", index, quality.roles[index], role)
		}
	}
	if got := router.Identity(); got.Model != quality.identity.Model || got.Profile != "safe-hybrid" {
		t.Fatalf("unexpected router identity: %#v", got)
	}
}

func TestSafeHybridRequiresBothClients(t *testing.T) {
	quality := &recordingLLM{}
	if _, err := NewSafeHybrid(nil, quality); err == nil {
		t.Fatal("missing fast model accepted")
	}
	if _, err := NewSafeHybrid(quality, nil); err == nil {
		t.Fatal("missing quality model accepted")
	}
}

func TestSafeHybridFallsBackToQualityWhenFastModelFails(t *testing.T) {
	fast := &recordingLLM{identity: aiport.ProviderIdentity{Model: "fast"}, err: errors.New("fast model unavailable")}
	quality := &recordingLLM{identity: aiport.ProviderIdentity{Model: "quality"}}
	router, err := NewSafeHybrid(fast, quality)
	if err != nil {
		t.Fatal(err)
	}
	response, err := router.Generate(context.Background(), aiport.StoryRequest{Role: "choices"})
	if err != nil {
		t.Fatal(err)
	}
	if string(response.Output) != `{"ok":true}` || len(fast.roles) != 1 || len(quality.roles) != 1 || quality.roles[0] != "choices" {
		t.Fatalf("fallback was not used: fast=%v quality=%v response=%s", fast.roles, quality.roles, response.Output)
	}
	if response.Provider.Model != "quality" || response.EscalatedFrom != "fast" {
		t.Fatalf("fallback provenance was not preserved: %#v", response)
	}
}

func TestSafeHybridDoesNotFallbackAfterCancellation(t *testing.T) {
	fast := &recordingLLM{err: context.Canceled}
	quality := &recordingLLM{}
	router, err := NewSafeHybrid(fast, quality)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = router.Generate(context.Background(), aiport.StoryRequest{Role: "choices"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(quality.roles) != 0 {
		t.Fatalf("quality fallback ran after cancellation: %v", quality.roles)
	}
}
