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
