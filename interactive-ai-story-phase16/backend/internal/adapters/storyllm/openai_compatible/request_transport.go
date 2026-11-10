package openai_compatible

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

const maxQuotaRetries = 3

// Equal jitter avoids immediate retries while spreading clients over time.
// Long Retry-After hints belong to the rejected credential/model. Short hints
// (up to 30s) also bound origin-wide recovery to avoid bursts on shared quota.
func quotaDelay(retry int, retryAfter string, now time.Time, jitter float64) time.Duration {
	ceiling := time.Second << min(max(retry, 0), 3)
	delay := ceiling/2 + time.Duration(float64(ceiling/2)*jitter)
	var requested time.Duration
	if seconds, err := strconv.ParseInt(strings.TrimSpace(retryAfter), 10, 64); err == nil && seconds > 0 {
		requested = time.Duration(min(seconds, 86400)) * time.Second
	} else if at, err := http.ParseTime(retryAfter); err == nil {
		requested = at.Sub(now)
	}
	if requested > delay {
		delay = requested
	}
	return min(delay, 24*time.Hour)
}

func (c *Client) bufferedRequest(ctx context.Context, body []byte, agent bool) ([]byte, error) {
	response, err := c.request(ctx, body, agent, false)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return io.ReadAll(response.Body) // request already bounded and buffered it
}

// Only pre-acceptance failures are retried. A successful stream keeps its slot
// until the consumer closes it and is never restarted after partial output.
func (c *Client) request(ctx context.Context, body []byte, agent, stream bool) (*http.Response, error) {
	state, _ := ctx.Value(requestStateKey{}).(*requestState)
	if state == nil {
		state = &requestState{}
	}
	keys := c.cfg.APIKeys
	eligibleKeys := keys
	if c.cfg.Provider != "google_gemini" && len(eligibleKeys) > 1 {
		eligibleKeys = eligibleKeys[:1] // credential rotation is Google-only
	}
	ringID := c.cfg.Provider + "\x00" + c.cfg.Model + "\x00" + strings.TrimRight(c.cfg.BaseURL, "/")
	index := keyCursor(ringID, len(keys))
	scope := requestScope(c.cfg.Provider, c.cfg.BaseURL)
	rejected := make(map[int]bool)
	for {
		release, err := c.cfg.Requests.acquire(ctx, scope, c.cfg.Provider == "google_gemini", state.background)
		if err != nil {
			return nil, err
		}
		var available bool
		index, available = c.cfg.Requests.availableCredential(scope, c.cfg.Model, eligibleKeys, index, rejected)
		if !available {
			release()
			return nil, fmt.Errorf("%w: configured credentials cooling down", ai.ErrUnavailable)
		}
		endpoint := chatCompletionsEndpoint(c.cfg.BaseURL)
		if agent {
			endpoint = interactionsEndpoint(c.cfg.BaseURL)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			release()
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if stream {
			req.Header.Set("Accept", "text/event-stream")
		}
		if agent {
			req.Header.Set("Api-Revision", "2026-05-20")
		}
		if len(keys) > 0 {
			if agent {
				req.Header.Set("x-goog-api-key", keys[index])
			} else {
				req.Header.Set("Authorization", "Bearer "+keys[index])
			}
		}
		resp, err := c.http.Do(req)
		if err != nil {
			release()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("%w: request transport", ai.ErrUnavailable)
		}
		if stream && resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
			resp.Body = &permitBody{ReadCloser: resp.Body, release: release}
			rememberKeyCursor(ringID, index, len(keys))
			return resp, nil
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		closeErr := resp.Body.Close()
		status := resp.StatusCode
		if status >= 200 && status < 300 && c.cfg.Provider == "google_gemini" {
			if embedded, found := embeddedGoogleErrorStatus(raw); found {
				status = embedded
			}
		}
		// Install cooldown before releasing the slot: queued work must not slip
		// through between the 429 response and its shared backoff.
		if status == http.StatusTooManyRequests && readErr == nil {
			now, jitter := time.Now(), rand.Float64()
			delay := quotaDelay(state.quotaRetries, "", now, jitter)
			hint := quotaDelay(state.quotaRetries, resp.Header.Get("Retry-After"), now, jitter)
			if c.cfg.Requests.backoff != nil {
				delay = c.cfg.Requests.backoff(state.quotaRetries, "")
			}
			if hint > quotaDelay(state.quotaRetries, "", now, jitter) {
				key := ""
				if len(keys) > 0 {
					key = keys[index]
				}
				c.cfg.Requests.coolCredential(scope, c.cfg.Model, key, hint)
				if hint <= 30*time.Second {
					delay = max(delay, hint)
				}
			}
			c.cfg.Requests.cooldown(scope, delay)
		}
		release()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if readErr != nil || closeErr != nil {
			return nil, fmt.Errorf("%w: response body", ai.ErrUnavailable)
		}
		if status >= 200 && status < 300 {
			resp.Body = io.NopCloser(bytes.NewReader(raw))
			rememberKeyCursor(ringID, index, len(keys))
			return resp, nil
		}
		if schemaUnsupported(status, raw) {
			return nil, fmt.Errorf("%w: %w", ai.ErrUnavailable, errSchemaUnsupported)
		}
		if stream && streamUnsupported(status, raw) {
			return nil, fmt.Errorf("%w: %w", ai.ErrUnavailable, errStreamUnsupported)
		}
		if status == http.StatusTooManyRequests {
			if state.quotaRetries >= maxQuotaRetries {
				return nil, fmt.Errorf("%w: quota retry budget exhausted (status 429)", ai.ErrUnavailable)
			}
			state.quotaRetries++
		} else if c.shouldRotateCredential(status, raw) {
			rejected[index] = true
			if len(rejected) >= len(keys) {
				return nil, fmt.Errorf("%w: credentials unavailable (status %d)", ai.ErrUnavailable, status)
			}
		} else {
			return nil, fmt.Errorf("%w: status %d", ai.ErrUnavailable, status)
		}
		if c.shouldRotateCredential(status, raw) && len(keys) > 0 {
			for {
				index = (index + 1) % len(keys)
				if !rejected[index] {
					break
				}
			}
			rememberKeyCursor(ringID, index, len(keys))
		}
	}
}
