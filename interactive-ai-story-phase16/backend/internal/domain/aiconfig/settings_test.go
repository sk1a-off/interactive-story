package aiconfig

import (
	"errors"
	"testing"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func googleCommand(model string) CreateRevisionCommand {
	return CreateRevisionCommand{
		StoryLLM:  aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: ProviderGoogleGemini, Model: model},
		Embedding: aiport.ProviderIdentity{Kind: aiport.KindEmbedding, Provider: "local_sentence_transformers", Model: "embedding"},
		Image:     aiport.ProviderIdentity{Kind: aiport.KindImage, Provider: "perchance_browser", Model: "image"},
		Public:    PublicSettings{StoryLLMEndpoint: GeminiOpenAIEndpoint, StoryLLMContext: 1048576, StoryLLMKVDType: "managed"},
	}
}

func TestGoogleStoryModelAllowlist(t *testing.T) {
	models := []string{
		Gemini36FlashModel,
		AntigravityModel,
		Gemini31FlashLiteModel,
		Gemini35FlashLiteModel,
		Gemma431BModel,
	}
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			if err := googleCommand(model).Validate(); err != nil {
				t.Fatalf("supported model rejected: %v", err)
			}
		})
	}
}

func TestGoogleStoryModelRejectsUnknownModel(t *testing.T) {
	if err := googleCommand("gemini-made-up").Validate(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("unknown Google model accepted: %v", err)
	}
}

func TestSafeHybridRequiresGoogleAntigravityAsQualityModel(t *testing.T) {
	valid := googleCommand(AntigravityModel)
	valid.Public.StoryLLMSafeHybrid = true
	if err := valid.Validate(); err != nil {
		t.Fatalf("safe hybrid rejected: %v", err)
	}

	wrongGoogleModel := googleCommand(Gemini35FlashLiteModel)
	wrongGoogleModel.Public.StoryLLMSafeHybrid = true
	if err := wrongGoogleModel.Validate(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("safe hybrid accepted without Antigravity quality model: %v", err)
	}

	local := googleCommand(AntigravityModel)
	local.StoryLLM.Provider = ProviderOpenAICompatible
	local.Public.StoryLLMEndpoint = "http://127.0.0.1:8081"
	local.Public.StoryLLMSafeHybrid = true
	if err := local.Validate(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("safe hybrid accepted for a local provider: %v", err)
	}
}
