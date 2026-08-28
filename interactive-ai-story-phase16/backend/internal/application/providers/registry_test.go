package providers

import (
	"context"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestRegistryResolvesByProviderModelProfile(t *testing.T) {
	r := NewRegistry()
	f := fakeai.NewStoryLLM([]byte(`{"ok":true}`))
	r.RegisterStory(f)
	got, err := r.Story(f.Identity())
	if err != nil {
		t.Fatal(err)
	}
	resp, err := got.Generate(context.Background(), aiport.StoryRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Output) != `{"ok":true}` {
		t.Fatalf("unexpected %s", resp.Output)
	}
	_, err = r.Story(aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "other", Model: "x"})
	if err != ErrProviderNotFound {
		t.Fatal("unknown provider must not silently fall back")
	}
}
