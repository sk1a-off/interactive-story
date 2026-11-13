package config

import (
	"testing"
	"time"
)

func TestFastStateSwitch(t *testing.T) {
	for _, value := range []string{"", "false", "true"} {
		t.Setenv("GENERATION_FAST_STATE_V1", value)
		cfg, err := Load()
		if err != nil || cfg.GenerationFastState != (value == "true") {
			t.Fatalf("value=%q err=%v", value, err)
		}
	}
}

func TestJournalEvidenceIsolationSwitch(t *testing.T) {
	for _, value := range []string{"", "false", "true"} {
		t.Setenv("GENERATION_JOURNAL_EVIDENCE_ISOLATION_V1", value)
		cfg, err := Load()
		if err != nil || cfg.GenerationJournalEvidenceIsolation != (value == "true") {
			t.Fatalf("value=%q cfg=%v err=%v", value, cfg.GenerationJournalEvidenceIsolation, err)
		}
	}
}

func TestGenerationExecutionTimeout(t *testing.T) {
	for value, want := range map[string]time.Duration{"": 25 * time.Minute, "30m": 30 * time.Minute, "1h": time.Hour} {
		t.Setenv("GENERATION_EXECUTION_TIMEOUT", value)
		cfg, err := Load()
		if err != nil || cfg.GenerationExecutionTimeout != want {
			t.Fatalf("value=%q got=%v err=%v, want %v", value, cfg.GenerationExecutionTimeout, err, want)
		}
	}
	for _, bad := range []string{"0", "-5m", "soon"} {
		t.Setenv("GENERATION_EXECUTION_TIMEOUT", bad)
		if _, err := Load(); err == nil {
			t.Fatalf("value=%q must be rejected", bad)
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("IMAGE_WORKER_TOKEN", " worker-secret ")
	t.Setenv("SECRET_STORE_PATH", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AppEnv != "development" {
		t.Fatalf("AppEnv = %q", cfg.AppEnv)
	}
	if cfg.HTTPAddr != "127.0.0.1:8787" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.SecretStorePath != "../data/secrets/provider-keys.json" {
		t.Fatalf("SecretStorePath = %q", cfg.SecretStorePath)
	}
	if cfg.ImageWorkerToken != "worker-secret" {
		t.Fatalf("ImageWorkerToken was not loaded and trimmed")
	}
}

func TestBrowserParallelism(t *testing.T) {
	for value, expected := range map[string]int{"": 32, "1": 1, "8": 8, "4096": 4096} {
		t.Setenv("CHATGPT_BROWSER_PARALLELISM", value)
		cfg, err := Load()
		if err != nil || cfg.ChatGPTBrowserParallelism != expected {
			t.Fatalf("value=%s cfg=%d err=%v", value, cfg.ChatGPTBrowserParallelism, err)
		}
	}
	for _, value := range []string{"0", "-1", "4097", "many"} {
		t.Setenv("CHATGPT_BROWSER_PARALLELISM", value)
		if _, err := Load(); err == nil {
			t.Fatal("invalid parallelism accepted", value)
		}
	}
}

func TestRemoteListenerRequiresTokensOrTrustedNetwork(t *testing.T) {
	t.Setenv("API_ACCESS_TOKEN", "")
	t.Setenv("IMAGE_WORKER_TOKEN", "")
	t.Setenv("HTTP_TRUSTED_NETWORK", "")
	for _, addr := range []string{"127.0.0.1:8787", "localhost:8787", "[::1]:8787"} {
		t.Setenv("HTTP_ADDR", addr)
		if _, err := Load(); err != nil {
			t.Fatalf("loopback %s must load without tokens: %v", addr, err)
		}
	}
	for _, addr := range []string{":8080", "0.0.0.0:8787", "10.10.10.1:8787", "[::]:8787"} {
		t.Setenv("HTTP_ADDR", addr)
		if _, err := Load(); err == nil {
			t.Fatalf("remote %s without tokens must be refused", addr)
		}
		t.Setenv("API_ACCESS_TOKEN", "api")
		if _, err := Load(); err == nil {
			t.Fatalf("remote %s with only the api token must be refused", addr)
		}
		t.Setenv("IMAGE_WORKER_TOKEN", "worker")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("remote %s with both tokens must load: %v", addr, err)
		}
		if cfg.ListensOnLoopback() {
			t.Fatalf("%s reported as loopback", addr)
		}
		t.Setenv("API_ACCESS_TOKEN", "")
		t.Setenv("IMAGE_WORKER_TOKEN", "")
		t.Setenv("HTTP_TRUSTED_NETWORK", "true")
		if _, err := Load(); err != nil {
			t.Fatalf("remote %s with HTTP_TRUSTED_NETWORK must load: %v", addr, err)
		}
		t.Setenv("HTTP_TRUSTED_NETWORK", "")
	}
}

func TestAPITokenRequiresWorkerTokenEvenOnLoopback(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:8787")
	t.Setenv("HTTP_TRUSTED_NETWORK", "")
	t.Setenv("API_ACCESS_TOKEN", "api")
	t.Setenv("IMAGE_WORKER_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("api token without worker token must be refused: worker routes would be exempt and open")
	}
	t.Setenv("IMAGE_WORKER_TOKEN", "worker")
	if _, err := Load(); err != nil {
		t.Fatalf("both tokens must load: %v", err)
	}
}
