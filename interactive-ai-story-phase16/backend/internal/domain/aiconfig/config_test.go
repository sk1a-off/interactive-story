package aiconfig

import (
	"errors"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestRevisionRequiresAllProviderKinds(t *testing.T) {
	i := id.MustParse("00000000-0000-4000-8000-000000000001")
	_, err := NewRevision(i, 1,
		aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "fake", Model: "story"},
		aiport.ProviderIdentity{Kind: aiport.KindEmbedding, Provider: "fake", Model: "embed"},
		aiport.ProviderIdentity{Kind: aiport.KindImage, Provider: "fake", Model: "image"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewRevision(i, 1, aiport.ProviderIdentity{}, aiport.ProviderIdentity{}, aiport.ProviderIdentity{})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatal("invalid config accepted")
	}
}
