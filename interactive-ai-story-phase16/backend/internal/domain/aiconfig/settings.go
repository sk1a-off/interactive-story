package aiconfig

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

var ErrInvalidPublicSettings = errors.New("invalid public provider settings")

const (
	ProviderOpenAICompatible = "openai_compatible"
	ProviderGoogleGemini     = "google_gemini"
	ProviderChatGPTBrowser   = "chatgpt_browser"
	ChatGPTBrowserModel      = "gpt-6"
	ChatGPTBrowserEndpoint   = "http://127.0.0.1:8791"
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
	GenerationProfile        GenerationProfile `json:"generationProfile"`
	StoryLLMBrowserReasoning string            `json:"storyLlmBrowserReasoning,omitempty"`
	StoryLLMBrowserRetries   *int              `json:"storyLlmBrowserRetries,omitempty"`
	StoryLLMEndpoint         string            `json:"storyLlmEndpoint"`
	StoryLLMContext          int               `json:"storyLlmContext"`
	StoryLLMKVDType          string            `json:"storyLlmKvDType"`
	StoryLLMStreamWriter     bool              `json:"storyLlmStreamWriter"`
	StoryLLMJSONSchema       bool              `json:"storyLlmJsonSchema"`
	StoryLLMSafeHybrid       bool              `json:"storyLlmSafeHybrid"`
	WorldRulesSafeMode       bool              `json:"worldRulesSafeMode"`
	EmbeddingEndpoint        string            `json:"embeddingEndpoint"`
	ImageEndpoint            string            `json:"imageEndpoint"`
	ImageResolution          string            `json:"image_resolution"`
	ImageCount               *int              `json:"image_count,omitempty"`
	MemoryEnabled            bool              `json:"memoryEnabled"`
	MemoryTopK               *int              `json:"memoryTopK,omitempty"`
	MemoryTokenBudget        *int              `json:"memoryTokenBudget,omitempty"`
}

func (s PublicSettings) Validate() error {
	if s.StoryLLMBrowserReasoning != "" && s.StoryLLMBrowserReasoning != "none" && s.StoryLLMBrowserReasoning != "medium" && s.StoryLLMBrowserReasoning != "high" {
		return ErrInvalidPublicSettings
	}
	if s.StoryLLMBrowserRetries != nil && (*s.StoryLLMBrowserRetries < 0 || *s.StoryLLMBrowserRetries > 2) {
		return ErrInvalidPublicSettings
	}
	if !s.GenerationProfile.Available() {
		return ErrInvalidPublicSettings
	}
	if s.MemoryTopK != nil && (*s.MemoryTopK < 1 || *s.MemoryTopK > 100) {
		return ErrInvalidPublicSettings
	}
	if s.MemoryTokenBudget != nil && (*s.MemoryTokenBudget < 256 || *s.MemoryTokenBudget > 8192) {
		return ErrInvalidPublicSettings
	}
	if s.ImageResolution != "" && s.ImageResolution != "768x768" && s.ImageResolution != "512x768" && s.ImageResolution != "768x512" {
		return ErrInvalidPublicSettings
	}
	if s.ImageCount != nil && (*s.ImageCount < 1 || *s.ImageCount > 4) {
		return ErrInvalidPublicSettings
	}
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
func (s PublicSettings) WithImageDefaults() PublicSettings {
	if s.ImageResolution == "" {
		s.ImageResolution = "768x768"
	}
	if s.ImageCount == nil {
		count := 2
		s.ImageCount = &count
	}
	return s
}

func (s PublicSettings) JSON() json.RawMessage {
	b, _ := json.Marshal(s.WithImageDefaults().WithMemoryDefaults().WithGenerationDefaults())
	return b
}

func (s PublicSettings) WithMemoryDefaults() PublicSettings {
	if s.MemoryTopK == nil {
		n := 8
		s.MemoryTopK = &n
	}
	if s.MemoryTokenBudget == nil {
		n := 1024
		s.MemoryTokenBudget = &n
	}
	return s
}

type CreateRevisionCommand struct {
	StoryLLM  aiport.ProviderIdentity `json:"storyLlm"`
	Embedding aiport.ProviderIdentity `json:"embedding"`
	Image     aiport.ProviderIdentity `json:"image"`
	Public    PublicSettings          `json:"public"`
}

func (c CreateRevisionCommand) Validate() error {
	if c.Public.MemoryEnabled && c.Embedding.Provider != "local_sentence_transformers" {
		return ErrInvalidPublicSettings
	}
	if c.StoryLLM.Kind != aiport.KindStoryLLM || c.StoryLLM.Provider == "" || c.StoryLLM.Model == "" ||
		c.Embedding.Kind != aiport.KindEmbedding || c.Embedding.Provider == "" || c.Embedding.Model == "" ||
		c.Image.Kind != aiport.KindImage || c.Image.Provider == "" || c.Image.Model == "" {
		return ErrInvalidConfig
	}
	if c.StoryLLM.Provider != ProviderOpenAICompatible && c.StoryLLM.Provider != ProviderGoogleGemini && c.StoryLLM.Provider != ProviderChatGPTBrowser {
		return ErrInvalidConfig
	}
	if c.StoryLLM.Provider == ProviderChatGPTBrowser {
		if c.StoryLLM.Model != ChatGPTBrowserModel || !IsBrowserEndpoint(c.Public.StoryLLMEndpoint) || c.Public.StoryLLMSafeHybrid || c.Public.StoryLLMStreamWriter {
			return ErrInvalidConfig
		}
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

// Local bridge credentials must never be sent to an external endpoint.
func IsBrowserEndpoint(endpoint string) bool {
	u, e := url.Parse(strings.TrimSpace(endpoint))
	if e != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return false
	}
	return u.Path == "" || u.Path == "/" || u.Path == "/v1" || u.Path == "/v1/chat/completions"
}
func (s PublicSettings) WithBrowserDefaults() PublicSettings {
	if s.StoryLLMBrowserReasoning == "" {
		s.StoryLLMBrowserReasoning = "none"
	}
	if s.StoryLLMBrowserRetries == nil {
		n := 1
		s.StoryLLMBrowserRetries = &n
	}
	return s
}
