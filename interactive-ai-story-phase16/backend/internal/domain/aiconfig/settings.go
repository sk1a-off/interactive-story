package aiconfig

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

var ErrInvalidPublicSettings = errors.New("invalid public provider settings")

const (
	ProviderOpenAICompatible = "openai_compatible"
	ProviderGoogleGemini     = "google_gemini"
	Gemini36FlashModel       = "gemini-3.6-flash"
	AntigravityModel         = "antigravity-preview-05-2026"
	Gemini31FlashLiteModel   = "gemini-3.1-flash-lite"
	Gemini35FlashLiteModel   = "gemini-3.5-flash-lite"
	Gemma431BModel           = "gemma-4-31b-it"
	GeminiFlashModel         = Gemini35FlashLiteModel
	GeminiOpenAIEndpoint     = "https://generativelanguage.googleapis.com/v1beta/openai"
)

var supportedGoogleStoryModels = map[string]struct{}{
	Gemini36FlashModel:     {},
	AntigravityModel:       {},
	Gemini31FlashLiteModel: {},
	Gemini35FlashLiteModel: {},
	Gemma431BModel:         {},
}

func IsSupportedGoogleStoryModel(model string) bool {
	_, ok := supportedGoogleStoryModels[model]
	return ok
}

type PublicSettings struct {
	StoryLLMEndpoint   string `json:"storyLlmEndpoint"`
	StoryLLMContext    int    `json:"storyLlmContext"`
	StoryLLMKVDType    string `json:"storyLlmKvDType"`
	StoryLLMSafeHybrid bool   `json:"storyLlmSafeHybrid"`
	WorldRulesSafeMode bool   `json:"worldRulesSafeMode"`
	EmbeddingEndpoint  string `json:"embeddingEndpoint"`
	ImageEndpoint      string `json:"imageEndpoint"`
}

func (s PublicSettings) Validate() error {
	if s.StoryLLMContext < 1024 || s.StoryLLMContext > 1048576 {
		return ErrInvalidPublicSettings
	}
	for _, v := range []string{s.StoryLLMEndpoint, s.EmbeddingEndpoint, s.ImageEndpoint} {
		if strings.TrimSpace(v) == "" {
			continue
		}
		u, e := url.Parse(v)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return ErrInvalidPublicSettings
		}
	}
	return nil
}
func (s PublicSettings) JSON() json.RawMessage { b, _ := json.Marshal(s); return b }

type CreateRevisionCommand struct {
	StoryLLM  aiport.ProviderIdentity `json:"storyLlm"`
	Embedding aiport.ProviderIdentity `json:"embedding"`
	Image     aiport.ProviderIdentity `json:"image"`
	Public    PublicSettings          `json:"public"`
}

func (c CreateRevisionCommand) Validate() error {
	if c.StoryLLM.Kind != aiport.KindStoryLLM || c.StoryLLM.Provider == "" || c.StoryLLM.Model == "" ||
		c.Embedding.Kind != aiport.KindEmbedding || c.Embedding.Provider == "" || c.Embedding.Model == "" ||
		c.Image.Kind != aiport.KindImage || c.Image.Provider == "" || c.Image.Model == "" {
		return ErrInvalidConfig
	}
	if c.StoryLLM.Provider != ProviderOpenAICompatible && c.StoryLLM.Provider != ProviderGoogleGemini {
		return ErrInvalidConfig
	}
	if c.StoryLLM.Provider == ProviderGoogleGemini {
		if !IsSupportedGoogleStoryModel(c.StoryLLM.Model) || strings.TrimRight(c.Public.StoryLLMEndpoint, "/") != GeminiOpenAIEndpoint {
			return ErrInvalidConfig
		}
	}
	if c.Public.StoryLLMSafeHybrid && (c.StoryLLM.Provider != ProviderGoogleGemini || c.StoryLLM.Model != AntigravityModel) {
		return ErrInvalidConfig
	}
	return c.Public.Validate()
}

type SafeView struct {
	ID                       string                  `json:"id"`
	Revision                 int64                   `json:"revision"`
	StoryLLM                 aiport.ProviderIdentity `json:"storyLlm"`
	Embedding                aiport.ProviderIdentity `json:"embedding"`
	Image                    aiport.ProviderIdentity `json:"image"`
	Public                   PublicSettings          `json:"public"`
	Active                   bool                    `json:"active"`
	StoryLLMSecretConfigured bool                    `json:"storyLlmSecretConfigured"`
	StoryLLMSecretCount      int                     `json:"storyLlmSecretCount"`
}
