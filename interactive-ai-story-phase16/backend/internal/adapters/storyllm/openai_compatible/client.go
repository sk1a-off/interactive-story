package openai_compatible

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	promptcatalog "github.com/local/interactive-ai-story/backend/prompts"
)

type Config struct {
	BaseURL   string
	APIKey    string
	APIKeys   []string
	Model     string
	Profile   string
	Provider  string
	Timeout   time.Duration
	PromptSet *promptset.Revision
}

var providerKeyCursor = struct {
	sync.Mutex
	values map[string]int
}{values: map[string]int{}}

type Client struct {
	cfg          Config
	http         *http.Client
	identity     aiport.ProviderIdentity
	prompts      map[string]string
	roleSettings map[string]promptset.RoleSettings
}

func New(cfg Config, hc *http.Client) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("invalid Story LLM base URL")
	}
	if cfg.Model == "" {
		return nil, errors.New("Story LLM model is required")
	}
	if cfg.Provider == "" {
		cfg.Provider = "openai_compatible"
	}
	cfg.APIKeys = normalizedKeys(cfg.APIKeys, cfg.APIKey)
	if cfg.Timeout <= 0 {
		cfg.Timeout = 120 * time.Second
	}
	if isAntigravity(cfg.Model) && cfg.Timeout < 300*time.Second {
		cfg.Timeout = 300 * time.Second
	}
	if hc == nil {
		hc = &http.Client{Timeout: cfg.Timeout}
	}

	// Snapshot the prompt revision into the client. Prompt-set revisions are
	// immutable in storage, and a client created for a queued generation must
	// keep using the exact revision that was resolved for that job. Copying the
	// maps also prevents an accidental caller mutation from changing a live
	// client's behavior.
	prompts := map[string]string{}
	roleSettings := map[string]promptset.RoleSettings{}
	if cfg.PromptSet != nil {
		for role, text := range cfg.PromptSet.Prompts {
			prompts[role] = text
		}
		for role, settings := range cfg.PromptSet.RoleSettings {
			roleSettings[role] = settings
		}
	}

	return &Client{
		cfg:          cfg,
		http:         hc,
		identity:     aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: cfg.Provider, Model: cfg.Model, Profile: cfg.Profile},
		prompts:      prompts,
		roleSettings: roleSettings,
	}, nil
}
func (c *Client) Identity() aiport.ProviderIdentity { return c.identity }

type chatRequest struct {
	Model           string         `json:"model"`
	Messages        []message      `json:"messages"`
	Temperature     float64        `json:"temperature"`
	TopP            float64        `json:"top_p"`
	MaxTokens       int            `json:"max_tokens,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	ResponseFormat  responseFormat `json:"response_format"`
}
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type responseFormat struct {
	Type string `json:"type"`
}
type chatResponse struct {
	Choices []struct {
		Message      message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

type interactionRequest struct {
	Agent             string                    `json:"agent"`
	Environment       string                    `json:"environment"`
	Input             string                    `json:"input"`
	SystemInstruction string                    `json:"system_instruction"`
	ResponseFormat    interactionResponseFormat `json:"response_format"`
}

type interactionResponseFormat struct {
	Type     string `json:"type"`
	MIMEType string `json:"mime_type"`
}

type interactionResponse struct {
	Status string `json:"status"`
	Steps  []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"steps"`
	Usage struct {
		InputTokens  int64 `json:"total_input_tokens"`
		OutputTokens int64 `json:"total_output_tokens"`
	} `json:"usage"`
}

func normalizeJSONObject(content string) ([]byte, bool) {
	s := strings.TrimSpace(content)

	if out, ok := validJSONObject(s); ok {
		return out, true
	}

	if !strings.HasPrefix(s, "```") || !strings.HasSuffix(s, "```") {
		return nil, false
	}

	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "```"), "```"))

	if strings.Contains(body, "```") {
		return nil, false
	}

	if i := strings.IndexByte(body, '\n'); i >= 0 {
		firstLine := strings.TrimSpace(body[:i])
		if strings.EqualFold(firstLine, "json") {
			body = strings.TrimSpace(body[i+1:])
		} else if firstLine != "" && !strings.HasPrefix(firstLine, "{") {
			return nil, false
		}
	}

	return validJSONObject(body)
}

func validJSONObject(s string) ([]byte, bool) {
	raw := []byte(strings.TrimSpace(s))
	if !json.Valid(raw) {
		return nil, false
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, false
	}

	return raw, true
}

func (c *Client) Generate(ctx context.Context, req aiport.StoryRequest) (aiport.StoryResponse, error) {
	role := strings.TrimSpace(req.Role)
	contract := strings.TrimSpace(c.prompts[role])
	if contract == "" {
		var err error
		contract, err = promptcatalog.Get(req.PromptVersion, role)
		if err != nil {
			return aiport.StoryResponse{}, err
		}
	}

	settings, ok := c.roleSettings[role]
	if !ok || settings.Validate() != nil {
		settings = promptset.DefaultRoleSettings(role)
	}
	maxTokens := settings.MaxTokens
	if req.MaxTokens >= 128 && req.MaxTokens < maxTokens {
		maxTokens = req.MaxTokens
	}

	system := contract + "\n\nReturn exactly one JSON object and nothing else. Do not wrap it in Markdown or code fences. Do not include chain-of-thought or hidden reasoning."
	if c.cfg.Provider == "google_gemini" && isAntigravity(c.cfg.Model) {
		return c.generateAntigravity(ctx, system, req.Input)
	}
	chat := chatRequest{
		Model:          c.cfg.Model,
		Messages:       []message{{Role: "system", Content: system}, {Role: "user", Content: string(req.Input)}},
		Temperature:    settings.Temperature,
		TopP:           settings.TopP,
		MaxTokens:      maxTokens,
		ResponseFormat: responseFormat{Type: "json_object"},
	}
	if c.cfg.Provider == "google_gemini" {
		chat.ReasoningEffort = googleReasoningEffort(c.cfg.Model)
	}
	body, err := json.Marshal(chat)
	if err != nil {
		return aiport.StoryResponse{}, err
	}
	raw, err := c.doWithCredentialRotation(ctx, body)
	if err != nil {
		return aiport.StoryResponse{}, err
	}
	var decoded chatResponse
	if err = json.Unmarshal(raw, &decoded); err != nil {
		return aiport.StoryResponse{}, fmt.Errorf("%w: response_json=%v", aiport.ErrInvalidOutput, err)
	}
	if len(decoded.Choices) != 1 {
		return aiport.StoryResponse{}, fmt.Errorf("%w: choices=%d", aiport.ErrInvalidOutput, len(decoded.Choices))
	}
	out, ok := normalizeJSONObject(decoded.Choices[0].Message.Content)
	if !ok {
		return aiport.StoryResponse{}, fmt.Errorf("%w: finish_reason=%s content_chars=%d", aiport.ErrInvalidOutput, decoded.Choices[0].FinishReason, len([]rune(decoded.Choices[0].Message.Content)))
	}
	return aiport.StoryResponse{Output: out, InputTokens: decoded.Usage.PromptTokens, OutputTokens: decoded.Usage.CompletionTokens, Provider: c.Identity()}, nil
}

func (c *Client) generateAntigravity(ctx context.Context, system string, input []byte) (aiport.StoryResponse, error) {
	body, err := json.Marshal(interactionRequest{
		Agent:             c.cfg.Model,
		Environment:       "remote",
		Input:             string(input),
		SystemInstruction: system + " Do not use tools; answer from the supplied story context only.",
		ResponseFormat:    interactionResponseFormat{Type: "text", MIMEType: "application/json"},
	})
	if err != nil {
		return aiport.StoryResponse{}, err
	}
	raw, err := c.doAntigravityWithCredentialRotation(ctx, body)
	if err != nil {
		return aiport.StoryResponse{}, err
	}
	var decoded interactionResponse
	if err = json.Unmarshal(raw, &decoded); err != nil {
		return aiport.StoryResponse{}, fmt.Errorf("%w: interaction_response_json=%v", aiport.ErrInvalidOutput, err)
	}
	if decoded.Status != "completed" {
		return aiport.StoryResponse{}, fmt.Errorf("%w: interaction_status=%s", aiport.ErrInvalidOutput, decoded.Status)
	}
	content := lastModelOutputText(decoded)
	out, ok := normalizeJSONObject(content)
	if !ok {
		return aiport.StoryResponse{}, fmt.Errorf("%w: interaction_content_chars=%d", aiport.ErrInvalidOutput, len([]rune(content)))
	}
	return aiport.StoryResponse{Output: out, InputTokens: decoded.Usage.InputTokens, OutputTokens: decoded.Usage.OutputTokens, Provider: c.Identity()}, nil
}

func lastModelOutputText(response interactionResponse) string {
	for stepIndex := len(response.Steps) - 1; stepIndex >= 0; stepIndex-- {
		step := response.Steps[stepIndex]
		if step.Type != "model_output" {
			continue
		}
		var parts []string
		for _, content := range step.Content {
			if content.Type == "text" && strings.TrimSpace(content.Text) != "" {
				parts = append(parts, content.Text)
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

func isAntigravity(model string) bool {
	return model == "antigravity-preview-05-2026"
}

func googleReasoningEffort(model string) string {
	if model == "gemma-4-31b-it" {
		return "minimal"
	}
	return "low"
}

func (c *Client) doWithCredentialRotation(ctx context.Context, body []byte) ([]byte, error) {
	keys := c.cfg.APIKeys
	attempts := len(keys)
	if attempts == 0 {
		attempts = 1
	}
	ringID := c.cfg.Provider + "\x00" + c.cfg.Model + "\x00" + strings.TrimRight(c.cfg.BaseURL, "/")
	start := keyCursor(ringID, len(keys))
	lastStatus := 0
	for offset := 0; offset < attempts; offset++ {
		index := 0
		key := ""
		if len(keys) > 0 {
			index = (start + offset) % len(keys)
			key = keys[index]
		}
		hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, chatCompletionsEndpoint(c.cfg.BaseURL), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		hreq.Header.Set("Content-Type", "application/json")
		if key != "" {
			hreq.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := c.http.Do(hreq)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", aiport.ErrUnavailable, err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if embeddedStatus, found := embeddedGoogleErrorStatus(raw); found {
				lastStatus = embeddedStatus
				if !c.shouldRotateCredential(embeddedStatus, raw) {
					return nil, fmt.Errorf("%w: embedded provider status %d", aiport.ErrUnavailable, embeddedStatus)
				}
				rememberKeyCursor(ringID, index+1, len(keys))
				continue
			}
			rememberKeyCursor(ringID, index, len(keys))
			return raw, nil
		}
		lastStatus = resp.StatusCode
		if !c.shouldRotateCredential(resp.StatusCode, raw) {
			return nil, fmt.Errorf("%w: status %d", aiport.ErrUnavailable, resp.StatusCode)
		}
		rememberKeyCursor(ringID, index+1, len(keys))
	}
	return nil, fmt.Errorf("%w: all configured Google AI API keys are exhausted or unavailable (last status %d)", aiport.ErrUnavailable, lastStatus)
}

func (c *Client) doAntigravityWithCredentialRotation(ctx context.Context, body []byte) ([]byte, error) {
	keys := c.cfg.APIKeys
	attempts := len(keys)
	if attempts == 0 {
		attempts = 1
	}
	ringID := c.cfg.Provider + "\x00" + c.cfg.Model + "\x00" + strings.TrimRight(c.cfg.BaseURL, "/")
	start := keyCursor(ringID, len(keys))
	lastStatus := 0
	for offset := 0; offset < attempts; offset++ {
		index := 0
		key := ""
		if len(keys) > 0 {
			index = (start + offset) % len(keys)
			key = keys[index]
		}
		hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, interactionsEndpoint(c.cfg.BaseURL), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		hreq.Header.Set("Content-Type", "application/json")
		hreq.Header.Set("Api-Revision", "2026-05-20")
		if key != "" {
			hreq.Header.Set("x-goog-api-key", key)
		}
		resp, err := c.http.Do(hreq)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", aiport.ErrUnavailable, err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			rememberKeyCursor(ringID, index, len(keys))
			return raw, nil
		}
		lastStatus = resp.StatusCode
		if !c.shouldRotateCredential(resp.StatusCode, raw) {
			return nil, fmt.Errorf("%w: status %d", aiport.ErrUnavailable, resp.StatusCode)
		}
		rememberKeyCursor(ringID, index+1, len(keys))
	}
	return nil, fmt.Errorf("%w: all configured Google AI API keys are exhausted or unavailable (last status %d)", aiport.ErrUnavailable, lastStatus)
}

func embeddedGoogleErrorStatus(raw []byte) (int, bool) {
	var envelopes []struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelopes); err != nil || len(envelopes) == 0 || envelopes[0].Error.Code == 0 {
		return 0, false
	}
	return envelopes[0].Error.Code, true
}

func (c *Client) shouldRotateCredential(status int, raw []byte) bool {
	if c.cfg.Provider != "google_gemini" {
		return false
	}
	if status == http.StatusTooManyRequests {
		return true
	}
	// A rejected/disabled key cannot recover through backoff. Skipping it keeps
	// the remaining configured credentials usable without retrying model or JSON
	// errors with a different identity.
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		text := strings.ToLower(string(raw))
		return strings.Contains(text, "api key") || strings.Contains(text, "permission_denied") || strings.Contains(text, "leaked")
	}
	return false
}

func normalizedKeys(values []string, legacy string) []string {
	if len(values) == 0 && strings.TrimSpace(legacy) != "" {
		values = []string{legacy}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func keyCursor(ringID string, size int) int {
	if size == 0 {
		return 0
	}
	providerKeyCursor.Lock()
	defer providerKeyCursor.Unlock()
	return providerKeyCursor.values[ringID] % size
}

func rememberKeyCursor(ringID string, index, size int) {
	if size == 0 {
		return
	}
	providerKeyCursor.Lock()
	providerKeyCursor.values[ringID] = index % size
	providerKeyCursor.Unlock()
}

// Local llama.cpp servers usually expose /v1 below their host root, while
// Google's documented OpenAI-compatible base URL already ends in /openai/.
// Supporting both shapes keeps the adapter protocol-compatible without
// inventing a second request/response implementation for Gemini.
func chatCompletionsEndpoint(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	path := ""
	if parsed, err := url.Parse(base); err == nil {
		path = strings.TrimRight(parsed.Path, "/")
	}
	if strings.HasSuffix(path, "/v1") || strings.HasSuffix(path, "/openai") {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

func interactionsEndpoint(baseURL string) string {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return strings.TrimRight(baseURL, "/") + "/interactions"
	}
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/openai") + "/interactions"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
