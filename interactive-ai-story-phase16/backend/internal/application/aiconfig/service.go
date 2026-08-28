package aiconfig

import (
	"context"
	"encoding/json"
	"errors"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/aiconfigrepo"
	"github.com/local/interactive-ai-story/backend/internal/ports/secrets"
	"strings"
)

var ErrGeminiAPIKeyRequired = errors.New("Google AI API key is required before enabling a Google model")

func SecretKey(revisionID id.ID) string { return "story_llm_api_key:" + revisionID.String() }

type Service struct {
	Repo    aiconfigrepo.Repository
	Secrets secrets.Store
}
type UpdateRequest struct {
	Config          domain.CreateRevisionCommand `json:"config"`
	StoryLLMAPIKey  *string                      `json:"storyLlmApiKey,omitempty"`
	StoryLLMAPIKeys []string                     `json:"storyLlmApiKeys,omitempty"`
	Activate        bool                         `json:"activate"`
}

func (s Service) List(ctx context.Context) ([]domain.SafeView, error) {
	rows, e := s.Repo.List(ctx)
	if e != nil {
		return nil, e
	}
	active, e := s.Repo.Active(ctx)
	if e != nil {
		return nil, e
	}
	out := make([]domain.SafeView, 0, len(rows))
	for _, v := range rows {
		out = append(out, s.safe(ctx, v, v.ID == active.ID))
	}
	return out, nil
}
func (s Service) Active(ctx context.Context) (domain.SafeView, error) {
	v, e := s.Repo.Active(ctx)
	if e != nil {
		return domain.SafeView{}, e
	}
	return s.safe(ctx, v, true), nil
}
func (s Service) Create(ctx context.Context, r UpdateRequest) (domain.SafeView, error) {
	if e := r.Config.Validate(); e != nil {
		return domain.SafeView{}, e
	}
	keys := normalizeAPIKeys(r.StoryLLMAPIKeys, r.StoryLLMAPIKey)
	if r.Config.StoryLLM.Provider == domain.ProviderGoogleGemini && len(keys) == 0 {
		current, currentErr := s.Repo.Active(ctx)
		canInherit := currentErr == nil && current.StoryLLM.Provider == domain.ProviderGoogleGemini && len(CredentialKeys(ctx, s.Secrets, current.ID)) > 0
		if !canInherit {
			return domain.SafeView{}, ErrGeminiAPIKeyRequired
		}
	}
	v, e := s.Repo.CreateRevision(ctx, r.Config)
	if e != nil {
		return domain.SafeView{}, e
	}
	if s.Secrets != nil {
		if len(keys) > 0 {
			if e = s.Secrets.Set(ctx, SecretKey(v.ID), encodeAPIKeys(keys)); e != nil {
				return domain.SafeView{}, e
			}
		} else if current, currentErr := s.Repo.Active(ctx); currentErr == nil && current.StoryLLM.Provider == r.Config.StoryLLM.Provider {
			if old, ok := s.Secrets.Get(ctx, SecretKey(current.ID)); ok {
				if e = s.Secrets.Set(ctx, SecretKey(v.ID), old); e != nil {
					return domain.SafeView{}, e
				}
			}
		}
	}
	if r.Activate {
		if e = s.Repo.Activate(ctx, v.ID); e != nil {
			return domain.SafeView{}, e
		}
	}
	return s.safe(ctx, v, r.Activate), nil
}
func (s Service) Activate(ctx context.Context, i id.ID) (domain.SafeView, error) {
	v, e := s.Repo.Get(ctx, i)
	if e != nil {
		return domain.SafeView{}, e
	}
	if v.StoryLLM.Provider == domain.ProviderGoogleGemini && len(CredentialKeys(ctx, s.Secrets, v.ID)) == 0 {
		return domain.SafeView{}, ErrGeminiAPIKeyRequired
	}
	if e = s.Repo.Activate(ctx, i); e != nil {
		return domain.SafeView{}, e
	}
	return s.safe(ctx, v, true), nil
}
func (s Service) safe(ctx context.Context, v domain.Revision, active bool) domain.SafeView {
	var pub domain.PublicSettings
	_ = json.Unmarshal(v.Settings, &pub)
	keyCount := len(CredentialKeys(ctx, s.Secrets, v.ID))
	return domain.SafeView{ID: v.ID.String(), Revision: v.Revision, StoryLLM: v.StoryLLM, Embedding: v.Embedding, Image: v.Image, Public: pub, Active: active, StoryLLMSecretConfigured: keyCount > 0, StoryLLMSecretCount: keyCount}
}

func CredentialKeys(ctx context.Context, store secrets.Store, revisionID id.ID) []string {
	if store == nil {
		return nil
	}
	raw, ok := store.Get(ctx, SecretKey(revisionID))
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	var keys []string
	if json.Unmarshal([]byte(raw), &keys) != nil {
		keys = []string{raw} // Backward compatibility with single-key revisions.
	}
	return normalizeAPIKeys(keys, nil)
}

func normalizeAPIKeys(values []string, legacy *string) []string {
	if len(values) == 0 && legacy != nil {
		values = []string{*legacy}
	}
	seen := map[string]struct{}{}
	keys := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func encodeAPIKeys(keys []string) string {
	if len(keys) == 1 {
		return keys[0]
	}
	raw, _ := json.Marshal(keys)
	return string(raw)
}
