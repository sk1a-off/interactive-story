package fakeai

import (
	"context"
	"testing"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestFakeProvidersAreDeterministic(t *testing.T) {
	e := NewEmbeddingProvider(4)
	a, _ := e.EmbedQueries(context.Background(), aiport.EmbeddingRequest{Texts: []string{"same"}})
	b, _ := e.EmbedQueries(context.Background(), aiport.EmbeddingRequest{Texts: []string{"same"}})
	for i := range a.Vectors[0] {
		if a.Vectors[0][i] != b.Vectors[0][i] {
			t.Fatal("embedding fake must be deterministic")
		}
	}
	img := NewImageProvider()
	x, _ := img.Generate(context.Background(), aiport.ImageRequest{Prompt: "p", Width: 10, Height: 20})
	y, _ := img.Generate(context.Background(), aiport.ImageRequest{Prompt: "p", Width: 10, Height: 20})
	if x.ArtifactRef != y.ArtifactRef {
		t.Fatal("image fake must be deterministic")
	}
}
