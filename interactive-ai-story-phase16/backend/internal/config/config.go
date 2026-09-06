package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	AppEnv                    string
	HTTPAddr                  string
	DatabaseURL               string
	StoryLLMBaseURL           string
	StoryLLMAPIKey            string
	StoryLLMModel             string
	StoryLLMProfile           string
	LocalOwnerID              string
	LocalUsername             string
	MediaRoot                 string
	MediaBaseURL              string
	SecretStorePath           string
	OmniVoiceURL              string
	GenerationContextShadow   bool
	GenerationProvisionalBeat bool
	EmbeddingBaseURL          string
	EmbeddingModel            string
}

func Load() (Config, error) {
	cfg := Config{
		AppEnv:                    envOrDefault("APP_ENV", "development"),
		HTTPAddr:                  envOrDefault("HTTP_ADDR", "127.0.0.1:8787"),
		DatabaseURL:               strings.TrimSpace(os.Getenv("DATABASE_URL")),
		StoryLLMBaseURL:           envOrDefault("STORY_LLM_BASE_URL", "http://127.0.0.1:8081"),
		StoryLLMAPIKey:            strings.TrimSpace(os.Getenv("STORY_LLM_API_KEY")),
		StoryLLMModel:             envOrDefault("STORY_LLM_MODEL", "Vikhr-Nemo-12B-Instruct-R-21-09-24-Q5_K_M"),
		StoryLLMProfile:           envOrDefault("STORY_LLM_PROFILE", "llamacpp-16k-kvq8"),
		LocalOwnerID:              envOrDefault("LOCAL_OWNER_ID", "00000000-0000-4000-8000-000000000001"),
		LocalUsername:             envOrDefault("LOCAL_USERNAME", "local"),
		MediaRoot:                 envOrDefault("MEDIA_ROOT", "./data/generated"),
		MediaBaseURL:              envOrDefault("MEDIA_BASE_URL", "/media"),
		SecretStorePath:           envOrDefault("SECRET_STORE_PATH", "../data/secrets/provider-keys.json"),
		OmniVoiceURL:              envOrDefault("OMNIVOICE_BASE_URL", "http://127.0.0.1:6655"),
		GenerationContextShadow:   envEnabled("GENERATION_CONTEXT_SHADOW_V1"),
		GenerationProvisionalBeat: envEnabled("GENERATION_PROVISIONAL_BEAT_V1"),
		EmbeddingBaseURL:          envOrDefault("EMBEDDING_BASE_URL", "http://127.0.0.1:8090"),
		EmbeddingModel:            envOrDefault("EMBEDDING_MODEL", "deepvk/USER2-small"),
	}
	if !strings.HasPrefix(cfg.HTTPAddr, ":") && !strings.Contains(cfg.HTTPAddr, ":") {
		return Config{}, fmt.Errorf("HTTP_ADDR must be a valid listen address: %q", cfg.HTTPAddr)
	}
	if !strings.HasPrefix(cfg.MediaBaseURL, "/") {
		return Config{}, fmt.Errorf("MEDIA_BASE_URL must be a local URL path")
	}
	return cfg, nil
}

func envEnabled(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
