package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	"net/http"
	"os"
	"strings"
	"time"

	storyllm "github.com/local/interactive-ai-story/backend/internal/adapters/storyllm/openai_compatible"
	"github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

type pairedOptions struct {
	Enabled, Smoke, Resume bool
	DSN, Secrets, Output   string
	Seed                   int64
}

type pairedModels struct {
	models         map[string]ai.StoryLLM
	config         aiconfig.Revision
	promptsID      string
	promptRevision int64
	prompts        promptset.Revision
	judges         map[string]ai.StoryLLM
}

func loadPairedModels(ctx context.Context, o pairedOptions) (pairedModels, error) {
	rev, prompts, keys, err := turnBenchmarkConfigured(ctx, turnBenchmarkOptions{ConfigDSN: o.DSN, SecretStore: o.Secrets})
	if err != nil {
		return pairedModels{}, err
	}
	if rev.StoryLLM.Provider != "google_gemini" {
		return pairedModels{}, errors.New("expected configured Google/Antigravity endpoint")
	}
	var settings aiconfig.PublicSettings
	if json.Unmarshal(rev.Settings, &settings) != nil {
		return pairedModels{}, errors.New("invalid configured settings")
	}
	token, err := os.ReadFile("../data/secrets/chatgpt-bridge-token")
	if err != nil {
		return pairedModels{}, errors.New("local bridge token unavailable")
	}
	models := map[string]ai.StoryLLM{}
	for _, arm := range []string{"chatgpt", "antigravity"} {
		cfg := storyllm.Config{BaseURL: settings.StoryLLMEndpoint, Provider: "google_gemini", Model: aiconfig.AntigravityModel, APIKeys: keys, PromptSet: &prompts, Timeout: 180 * time.Second, Requests: storyllm.NewRequestControl(3, 3)}
		if arm == "chatgpt" {
			cfg.BaseURL = "http://127.0.0.1:8791/v1/chat/completions"
			cfg.Provider = "openai_compatible"
			cfg.Model = "gpt-6-instant"
			cfg.APIKeys = []string{strings.TrimSpace(string(token))}
		}
		client, e := storyllm.New(cfg, &http.Client{Timeout: 180 * time.Second, Transport: pairedTransport{base: turnTransport{base: http.DefaultTransport}}})
		if e != nil {
			return pairedModels{}, e
		}
		models[arm] = client
	}
	judges := map[string]ai.StoryLLM{}
	for arm, base := range models {
		promptCopy := prompts
		promptCopy.Prompts = map[string]string{}
		for k, v := range prompts.Prompts {
			promptCopy.Prompts[k] = v
		}
		promptCopy.Prompts["paired_judge"] = pairedJudgeContract
		cfg := storyllm.Config{BaseURL: settings.StoryLLMEndpoint, Provider: "google_gemini", Model: aiconfig.AntigravityModel, APIKeys: keys, PromptSet: &promptCopy, Timeout: 180 * time.Second, Requests: storyllm.NewRequestControl(3, 3)}
		if arm == "chatgpt" {
			cfg.BaseURL = "http://127.0.0.1:8791/v1/chat/completions"
			cfg.Provider = "openai_compatible"
			cfg.Model = base.Identity().Model
			cfg.APIKeys = []string{strings.TrimSpace(string(token))}
		}
		j, e := storyllm.New(cfg, &http.Client{Timeout: 180 * time.Second, Transport: pairedTransport{base: turnTransport{base: http.DefaultTransport}}})
		if e != nil {
			return pairedModels{}, e
		}
		judges[arm] = j
	}
	return pairedModels{models: models, judges: judges, prompts: prompts, config: rev, promptsID: prompts.ID.String(), promptRevision: prompts.Revision}, nil
}
func runPairedSmoke(o pairedOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	m, err := loadPairedModels(ctx, o)
	if err != nil {
		return err
	}
	fmt.Printf("paired baseline model=%s config_revision=%d prompt_revision=%d\n", aiconfig.AntigravityModel, m.config.Revision, m.promptRevision)
	var tc testCase
	for _, c := range browserRoleSuite() {
		if c.Name == "director" {
			tc = c
		}
	}
	input, _ := json.Marshal(tc.Input)
	for _, arm := range []string{"antigravity", "chatgpt"} {
		start := time.Now()
		response, e := m.models[arm].Generate(ctx, ai.StoryRequest{Role: tc.Role, PromptVersion: "v1", Input: input, MaxTokens: tc.MaxTokens})
		fmt.Printf("smoke arm=%s elapsed_ms=%d valid=%t\n", arm, time.Since(start).Milliseconds(), e == nil && json.Valid(response.Output))
		if e != nil {
			return fmt.Errorf("%s smoke failed (provider details omitted)", arm)
		}
	}
	return nil
}
