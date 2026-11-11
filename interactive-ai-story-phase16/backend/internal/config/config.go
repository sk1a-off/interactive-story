package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	AppEnv                             string
	HTTPAddr                           string
	DatabaseURL                        string
	ChatGPTBridgeTokenFile             string
	StoryLLMBaseURL                    string
	StoryLLMAPIKey                     string
	StoryLLMModel                      string
	StoryLLMProfile                    string
	LocalOwnerID                       string
	LocalUsername                      string
	MediaRoot                          string
	MediaBaseURL                       string
	ImageWorkerToken                   string
	SecretStorePath                    string
	OmniVoiceURL                       string
	GenerationContextShadow            bool
	GenerationTurnPlanner              bool
	GenerationProvisionalBeat          bool
	GenerationJournalEvidenceIsolation bool
	GenerationFastState                bool
	EmbeddingBaseURL                   string
	EmbeddingModel                     string
	// GenerationExecutionTimeout is the per-attempt budget of a story worker.
	// It must exceed the largest story LLM timeout (safe-hybrid fast: 20m).
	GenerationExecutionTimeout time.Duration
}

// DefaultGenerationExecutionTimeout leaves headroom above the 20m safe-hybrid
// fast LLM timeout so a legitimate long request is not cut off by the worker.
const DefaultGenerationExecutionTimeout = 25 * time.Minute

func Load() (Config, error) {
	cfg := Config{
		ChatGPTBridgeTokenFile:             envOrDefault("CHATGPT_BRIDGE_TOKEN_FILE", "../data/secrets/chatgpt-bridge-token"),
		AppEnv:                             envOrDefault("APP_ENV", "development"),
		HTTPAddr:                           envOrDefault("HTTP_ADDR", "127.0.0.1:8787"),
		DatabaseURL:                        strings.TrimSpace(os.Getenv("DATABASE_URL")),
		StoryLLMBaseURL:                    envOrDefault("STORY_LLM_BASE_URL", "http://127.0.0.1:8081"),
		StoryLLMAPIKey:                     strings.TrimSpace(os.Getenv("STORY_LLM_API_KEY")),
		StoryLLMModel:                      envOrDefault("STORY_LLM_MODEL", "Vikhr-Nemo-12B-Instruct-R-21-09-24-Q5_K_M"),
		StoryLLMProfile:                    envOrDefault("STORY_LLM_PROFILE", "llamacpp-16k-kvq8"),
		LocalOwnerID:                       envOrDefault("LOCAL_OWNER_ID", "00000000-0000-4000-8000-000000000001"),
		LocalUsername:                      envOrDefault("LOCAL_USERNAME", "local"),
		MediaRoot:                          envOrDefault("MEDIA_ROOT", "./data/generated"),
		MediaBaseURL:                       envOrDefault("MEDIA_BASE_URL", "/media"),
		ImageWorkerToken:                   strings.TrimSpace(os.Getenv("IMAGE_WORKER_TOKEN")),
		SecretStorePath:                    envOrDefault("SECRET_STORE_PATH", "../data/secrets/provider-keys.json"),
		OmniVoiceURL:                       envOrDefault("OMNIVOICE_BASE_URL", "http://127.0.0.1:6655"),
		GenerationContextShadow:            envEnabled("GENERATION_CONTEXT_SHADOW_V1"),
		GenerationTurnPlanner:              envEnabled("GENERATION_TURN_PLANNER_V1"),
		GenerationProvisionalBeat:          envEnabled("GENERATION_PROVISIONAL_BEAT_V1"),
		GenerationJournalEvidenceIsolation: envEnabled("GENERATION_JOURNAL_EVIDENCE_ISOLATION_V1"),
		GenerationFastState:                envEnabled("GENERATION_FAST_STATE_V1"),
		EmbeddingBaseURL:                   envOrDefault("EMBEDDING_BASE_URL", "http://127.0.0.1:8090"),
		EmbeddingModel:                     envOrDefault("EMBEDDING_MODEL", "deepvk/USER2-small"),
	}
	if !strings.HasPrefix(cfg.HTTPAddr, ":") && !strings.Contains(cfg.HTTPAddr, ":") {
		return Config{}, fmt.Errorf("HTTP_ADDR must be a valid listen address: %q", cfg.HTTPAddr)
	}
	if !strings.HasPrefix(cfg.MediaBaseURL, "/") {
		return Config{}, fmt.Errorf("MEDIA_BASE_URL must be a local URL path")
	}
	timeout, err := time.ParseDuration(envOrDefault("GENERATION_EXECUTION_TIMEOUT", DefaultGenerationExecutionTimeout.String()))
	if err != nil {
		return Config{}, fmt.Errorf("GENERATION_EXECUTION_TIMEOUT must be a Go duration (e.g. 25m): %w", err)
	}
	if timeout <= 0 {
		return Config{}, fmt.Errorf("GENERATION_EXECUTION_TIMEOUT must be > 0, got %s", timeout)
	}
	cfg.GenerationExecutionTimeout = timeout
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
