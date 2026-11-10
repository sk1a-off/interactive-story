package openai_compatible

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

var errStreamUnsupported = errors.New("provider explicitly rejected streaming")
var errSSEFinished = errors.New("SSE terminal marker")

func streamUnsupported(status int, raw []byte) bool {
	if status != 400 && status != 422 {
		return false
	}
	text := strings.ToLower(string(raw))
	if !strings.Contains(text, "stream") {
		return false
	}
	for _, marker := range []string{"unsupported", "not supported", "not allowed", "unknown field", "unknown name", "unrecognized"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// Credential retries happen only before a stream is accepted. A partial stream
// is never concatenated with a replacement request or a different credential.
func (c *Client) openWriterStream(ctx context.Context, body []byte, agent bool) (*http.Response, error) {
	keys := c.cfg.APIKeys
	attempts := len(keys)
	if attempts == 0 {
		attempts = 1
	}
	ringID := c.cfg.Provider + "\x00" + c.cfg.Model + "\x00" + strings.TrimRight(c.cfg.BaseURL, "/")
	start := keyCursor(ringID, len(keys))
	lastStatus := 0
	for offset := 0; offset < attempts; offset++ {
		index, key := 0, ""
		if len(keys) > 0 {
			index = (start + offset) % len(keys)
			key = keys[index]
		}
		endpoint := chatCompletionsEndpoint(c.cfg.BaseURL)
		if agent {
			endpoint = interactionsEndpoint(c.cfg.BaseURL)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		if agent {
			req.Header.Set("Api-Revision", "2026-05-20")
			if key != "" {
				req.Header.Set("x-goog-api-key", key)
			}
		} else if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("%w: streaming transport", ai.ErrUnavailable)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
			rememberKeyCursor(ringID, index, len(keys))
			return resp, nil
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		status := resp.StatusCode
		if status >= 200 && status < 300 {
			if embedded, found := embeddedGoogleErrorStatus(raw); found {
				status = embedded
			} else {
				resp.Body = io.NopCloser(bytes.NewReader(raw))
				rememberKeyCursor(ringID, index, len(keys))
				return resp, nil
			}
		}
		if schemaUnsupported(status, raw) {
			return nil, fmt.Errorf("%w: %w", ai.ErrUnavailable, errSchemaUnsupported)
		}
		if streamUnsupported(status, raw) {
			return nil, fmt.Errorf("%w: %w", ai.ErrUnavailable, errStreamUnsupported)
		}
		lastStatus = status
		if !c.shouldRotateCredential(status, raw) {
			return nil, fmt.Errorf("%w: streaming status %d", ai.ErrUnavailable, status)
		}
		rememberKeyCursor(ringID, index+1, len(keys))
	}
	return nil, fmt.Errorf("%w: streaming credentials unavailable (last status %d)", ai.ErrUnavailable, lastStatus)
}
func consumeWriterSSE(ctx context.Context, reader io.Reader, handle func(string, []byte) error) error {
	scanner := bufio.NewScanner(io.LimitReader(reader, (4<<20)+1))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var name string
	var data bytes.Buffer
	total := 0
	dispatch := func() error {
		if data.Len() == 0 {
			return nil
		}
		raw := bytes.TrimSuffix(data.Bytes(), []byte("\n"))
		err := handle(name, raw)
		data.Reset()
		name = ""
		return err
	}
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Text()
		total += len(line) + 1
		if total > 4<<20 {
			return errors.New("writer SSE exceeds limit")
		}
		if line == "" {
			if err := dispatch(); err != nil {
				if errors.Is(err, errSSEFinished) {
					return nil
				}
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			value = strings.TrimPrefix(value, " ")
			data.WriteString(value)
			data.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := dispatch(); err != nil && !errors.Is(err, errSSEFinished) {
		return err
	}
	return nil
}
