package narration

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
	"time"
)

const (
	MaxTextChars  = 4000
	maxAudioBytes = 64 << 20
)

var ErrInvalidText = errors.New("narration text is invalid")

type Health struct {
	Status      string `json:"status"`
	Model       string `json:"model,omitempty"`
	Device      string `json:"device,omitempty"`
	GPU         string `json:"gpu,omitempty"`
	NumStep     int    `json:"numStep,omitempty"`
	VoicePrompt string `json:"voicePrompt,omitempty"`
}

type Service struct {
	baseURL string
	client  *http.Client
}

func NewService(baseURL string, client *http.Client) (*Service, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid OmniVoice base URL %q", baseURL)
	}
	if parsed.User != nil {
		return nil, errors.New("OmniVoice base URL must not contain credentials")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	return &Service{baseURL: baseURL, client: client}, nil
}

func (s *Service) Health(ctx context.Context) (Health, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/health", nil)
	if err != nil {
		return Health{}, err
	}
	res, err := s.client.Do(req)
	if err != nil {
		return Health{}, fmt.Errorf("OmniVoice unavailable: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Health{}, upstreamError(res)
	}
	var raw struct {
		Status      string `json:"status"`
		Model       string `json:"model"`
		Device      string `json:"device"`
		GPU         string `json:"gpu"`
		NumStep     int    `json:"num_step"`
		VoicePrompt string `json:"voice_prompt"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&raw); err != nil {
		return Health{}, fmt.Errorf("invalid OmniVoice health response: %w", err)
	}
	return Health{Status: raw.Status, Model: raw.Model, Device: raw.Device, GPU: raw.GPU, NumStep: raw.NumStep, VoicePrompt: raw.VoicePrompt}, nil
}

func (s *Service) Speech(ctx context.Context, text string) ([]byte, string, error) {
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > MaxTextChars {
		return nil, "", ErrInvalidText
	}
	payload, err := json.Marshal(map[string]any{
		"model":           "omnivoice",
		"input":           text,
		"voice":           "russian-reference",
		"response_format": "wav",
		"speed":           1.0,
	})
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/v1/audio/speech", bytes.NewReader(payload))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("OmniVoice synthesis unavailable: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", upstreamError(res)
	}
	audio, err := io.ReadAll(io.LimitReader(res.Body, maxAudioBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(audio) == 0 || len(audio) > maxAudioBytes {
		return nil, "", errors.New("OmniVoice returned invalid audio size")
	}
	contentType := strings.TrimSpace(res.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "audio/wav"
	}
	return audio, contentType, nil
}

func upstreamError(res *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 16<<10))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		detail = res.Status
	}
	return fmt.Errorf("OmniVoice returned %d: %s", res.StatusCode, detail)
}
