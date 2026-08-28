package aiconfig

import (
	"encoding/json"
	"errors"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

var ErrInvalidConfig = errors.New("invalid ai configuration")

type Revision struct {
	ID        id.ID
	Revision  int64
	StoryLLM  aiport.ProviderIdentity
	Embedding aiport.ProviderIdentity
	Image     aiport.ProviderIdentity
	Settings  json.RawMessage
}

func NewRevision(idv id.ID, revision int64, story, embedding, image aiport.ProviderIdentity) (Revision, error) {
	if idv.IsZero() || revision < 1 ||
		story.Kind != aiport.KindStoryLLM || story.Provider == "" || story.Model == "" ||
		embedding.Kind != aiport.KindEmbedding || embedding.Provider == "" || embedding.Model == "" ||
		image.Kind != aiport.KindImage || image.Provider == "" || image.Model == "" {
		return Revision{}, ErrInvalidConfig
	}
	return Revision{ID: idv, Revision: revision, StoryLLM: story, Embedding: embedding, Image: image, Settings: json.RawMessage(`{}`)}, nil
}
