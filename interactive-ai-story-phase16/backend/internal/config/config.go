package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv                    string
	HTTPAddr                  string
	DatabaseURL               string
	ChatGPTBridgeTokenFile    string
	ChatGPTBrowserParallelism int
	StoryLLMBaseURL           string
	StoryLLMAPIKey            string
	StoryLLMModel             string
	StoryLLMProfile           string
	LocalOwnerID              string
	LocalUsername             string
	MediaRoot                 string
	MediaBaseURL              string
	ImageWorkerToken          string
	// APIAccessToken, when set, is required on every route except health
	// checks (Bearer header or the session cookie). Mandatory for non-loopback
	// listeners unless TrustedNetwork acknowledges the exposure.
	APIAccessToken string
	// TrustedNetwork allows a non-loopback listener without tokens, for
	// deployments where reachability is restricted elsewhere (for example a
	// container whose port is published on 127.0.0.1 only).
	TrustedNetwork                     bool
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
		APIAccessToken:                     strings.TrimSpace(os.Getenv("API_ACCESS_TOKEN")),
		TrustedNetwork:                     envEnabled("HTTP_TRUSTED_NETWORK"),
		SecretStorePath:                    envOrDefault("SECRET_STORE_PATH", "../data/secrets/provider-keys.json"),
		OmniVoiceURL:                       envOrDefault("OMNIVOICE_BASE_URL", "http://127.0.0.1:6655"),
		GenerationContextShadow:            envEnabled("GENERATION_CONTEXT_SHADOW_V1"),
		GenerationTurnPlanner:              envEnabledDefault("GENERATION_TURN_PLANNER_V1", true),
		GenerationProvisionalBeat:          envEnabledDefault("GENERATION_PROVISIONAL_BEAT_V1", true),
		GenerationJournalEvidenceIsolation: envEnabled("GENERATION_JOURNAL_EVIDENCE_ISOLATION_V1"),
		GenerationFastState:                envEnabled("GENERATION_FAST_STATE_V1"),
		EmbeddingBaseURL:                   envOrDefault("EMBEDDING_BASE_URL", "http://127.0.0.1:8090"),
		EmbeddingModel:                     envOrDefault("EMBEDDING_MODEL", "deepvk/USER2-small"),
	}
	parallelism, parallelErr := strconv.Atoi(envOrDefault("CHATGPT_BROWSER_PARALLELISM", "32"))
	if parallelErr != nil || parallelism < 1 || parallelism > 4096 {
		return Config{}, fmt.Errorf("CHATGPT_BROWSER_PARALLELISM must be 1..4096")
	}
	cfg.ChatGPTBrowserParallelism = parallelism
	if !strings.HasPrefix(cfg.HTTPAddr, ":") && !strings.Contains(cfg.HTTPAddr, ":") {
		return Config{}, fmt.Errorf("HTTP_ADDR must be a valid listen address: %q", cfg.HTTPAddr)
	}
	if err := cfg.validateExposure(); err != nil {
		return Config{}, err
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

// ListensOnLoopback reports whether HTTPAddr can only be reached from the
// local machine. An empty or wildcard host (":8080", "0.0.0.0", "[::]")
// listens on every interface.
func (c Config) ListensOnLoopback() bool {
	return hostIsLoopback(c.HTTPAddr)
}

func hostIsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return false
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validateExposure refuses to expose an unauthenticated API beyond the local
// machine. Remote access needs both tokens, or an explicit acknowledgement
// that the network itself restricts who can reach the listener.
func (c Config) validateExposure() error {
	// With an API token the worker routes are exempted from it, so they must
	// carry their own token; otherwise the userscript would be the only
	// unauthenticated client and the exemption would reopen those routes.
	if c.APIAccessToken != "" && c.ImageWorkerToken == "" {
		return fmt.Errorf("API_ACCESS_TOKEN is set: IMAGE_WORKER_TOKEN must be set too so /internal/image-worker/* stays protected")
	}
	if c.ListensOnLoopback() || c.TrustedNetwork {
		return nil
	}
	var missing []string
	if c.APIAccessToken == "" {
		missing = append(missing, "API_ACCESS_TOKEN")
	}
	if c.ImageWorkerToken == "" {
		missing = append(missing, "IMAGE_WORKER_TOKEN")
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("HTTP_ADDR=%q is reachable from other hosts: set %s, or set HTTP_TRUSTED_NETWORK=true if the network already restricts access (see docs/implementation/RUN_LOCAL.md, remote access)", c.HTTPAddr, strings.Join(missing, " and "))
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

// envEnabledDefault is envEnabled with an explicit default for unset or blank
// values; "false"/"0"/"no"/"off" still disable the switch.
func envEnabledDefault(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
