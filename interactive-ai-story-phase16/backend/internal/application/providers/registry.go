package providers

import (
	"errors"
	"sync"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

var ErrProviderNotFound = errors.New("provider not found")

type Registry struct {
	mu        sync.RWMutex
	story     map[string]aiport.StoryLLM
	embedding map[string]aiport.EmbeddingProvider
	image     map[string]aiport.ImageProvider
}

func NewRegistry() *Registry {
	return &Registry{story: map[string]aiport.StoryLLM{}, embedding: map[string]aiport.EmbeddingProvider{}, image: map[string]aiport.ImageProvider{}}
}
func key(v aiport.ProviderIdentity) string { return v.Provider + "\x00" + v.Model + "\x00" + v.Profile }
func (r *Registry) RegisterStory(v aiport.StoryLLM) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.story[key(v.Identity())] = v
}
func (r *Registry) RegisterEmbedding(v aiport.EmbeddingProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.embedding[key(v.Identity())] = v
}
func (r *Registry) RegisterImage(v aiport.ImageProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.image[key(v.Identity())] = v
}
func (r *Registry) Story(v aiport.ProviderIdentity) (aiport.StoryLLM, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.story[key(v)]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return p, nil
}
func (r *Registry) Embedding(v aiport.ProviderIdentity) (aiport.EmbeddingProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.embedding[key(v)]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return p, nil
}
func (r *Registry) Image(v aiport.ProviderIdentity) (aiport.ImageProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.image[key(v)]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return p, nil
}
