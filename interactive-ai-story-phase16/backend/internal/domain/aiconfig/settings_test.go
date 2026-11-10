package aiconfig

import (
	"encoding/json"
	"errors"
	"testing"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestMemorySettingsDefaultOffAndBounded(t *testing.T) {
	defaults := (PublicSettings{}).WithMemoryDefaults()
	if defaults.MemoryEnabled || *defaults.MemoryTopK != 8 || *defaults.MemoryTokenBudget != 1024 {
		t.Fatal("unsafe memory defaults")
	}
	for _, n := range []int{0, -1, 101} {
		s := PublicSettings{StoryLLMContext: 16384, MemoryTopK: &n}
		if s.Validate() == nil {
			t.Fatalf("top-k accepted %d", n)
		}
	}
	for _, n := range []int{0, 255, 8193} {
		s := PublicSettings{StoryLLMContext: 16384, MemoryTokenBudget: &n}
		if s.Validate() == nil {
			t.Fatalf("budget accepted %d", n)
		}
	}
	for _, n := range []int{256, 1024, 8192} {
		s := PublicSettings{StoryLLMContext: 16384, MemoryTokenBudget: &n}
		if s.Validate() != nil {
			t.Fatalf("valid budget rejected %d", n)
		}
	}
}

func TestImageSettingsValidationAndLegacyDefaults(t *testing.T) {
	for _, resolution := range []string{"", "768x768", "512x768", "768x512"} {
		for count := 1; count <= 4; count++ {
			public := PublicSettings{StoryLLMContext: 16384, ImageResolution: resolution, ImageCount: &count}
			if err := public.Validate(); err != nil {
				t.Fatalf("rejected %s count=%d: %v", resolution, count, err)
			}
		}
	}
	for _, resolution := range []string{"512x512", "1024x768", "768X768"} {
		if err := (PublicSettings{StoryLLMContext: 16384, ImageResolution: resolution}).Validate(); err == nil {
			t.Fatalf("accepted invalid resolution %s", resolution)
		}
	}
	for _, count := range []int{-1, 0, 5} {
		if err := (PublicSettings{StoryLLMContext: 16384, ImageCount: &count}).Validate(); err == nil {
			t.Fatalf("accepted invalid count %d", count)
		}
	}
	var legacy PublicSettings
	if err := json.Unmarshal([]byte(`{"storyLlmContext":16384}`), &legacy); err != nil {
		t.Fatal(err)
	}
	var persisted PublicSettings
	if err := json.Unmarshal(legacy.JSON(), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.ImageResolution != "768x768" || persisted.ImageCount == nil || *persisted.ImageCount != 2 {
		t.Fatalf("legacy defaults = %s", legacy.JSON())
	}
}

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

func TestJSONSchemaSettingsLegacyOffAndRoundTrip(t *testing.T) {
	var legacy PublicSettings
	if err := json.Unmarshal([]byte(`{"storyLlmContext":16384}`), &legacy); err != nil || legacy.StoryLLMJSONSchema {
		t.Fatal("legacy schema mode changed")
	}
	for _, enabled := range []bool{false, true} {
		legacy.StoryLLMJSONSchema = enabled
		var decoded PublicSettings
		if json.Unmarshal(legacy.JSON(), &decoded) != nil || decoded.StoryLLMJSONSchema != enabled {
			t.Fatal("schema setting lost")
		}
	}
}

func TestWriterStreamingLegacyOffAndRoundTrip(t *testing.T) {
	var legacy PublicSettings
	if json.Unmarshal([]byte(`{"storyLlmContext":16384}`), &legacy) != nil || legacy.StoryLLMStreamWriter {
		t.Fatal("legacy streaming changed")
	}
	for _, enabled := range []bool{false, true} {
		legacy.StoryLLMStreamWriter = enabled
		var decoded PublicSettings
		if json.Unmarshal(legacy.JSON(), &decoded) != nil || decoded.StoryLLMStreamWriter != enabled {
			t.Fatal("streaming setting lost")
		}
	}
}
