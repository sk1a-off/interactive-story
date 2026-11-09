package config

import "testing"

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

func TestLoadDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DATABASE_URL", "")
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
}
