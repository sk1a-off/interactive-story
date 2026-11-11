package openai_compatible

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func validBrowserEffort(value string) bool {
	return value == "" || value == "none" || value == "medium" || value == "high"
}
func BrowserTimeout(effort string) time.Duration {
	switch effort {
	case "high":
		return 10 * time.Minute
	case "medium":
		return 5 * time.Minute
	default:
		return 3 * time.Minute
	}
}
func LoadBrowserToken(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("local ChatGPT bridge token unavailable; start the bridge or enter its token in AI settings")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("cannot read local bridge token")
	}
	token := strings.TrimSpace(string(raw))
	decoded, err := hex.DecodeString(token)
	if err != nil || len(decoded) != 24 {
		return "", errors.New("invalid local bridge token")
	}
	return token, nil
}

// Retries concern one read-only LLM call. No draft callback or Canon write runs
// before validation, and unknown transport outcomes/cancellation are not retried.
func (c *Client) Generate(ctx context.Context, req ai.StoryRequest) (ai.StoryResponse, error) {
	if c.cfg.Provider != "chatgpt_browser" {
		return c.generateOnce(ctx, req)
	}
	originalStreaming := req.Role == "writer" && req.OnText != nil
	req.OnText = nil
	var response ai.StoryResponse
	var err error
	for attempt := 0; attempt <= c.cfg.BrowserRetries; attempt++ {
		if ctx.Err() != nil {
			return response, ctx.Err()
		}
		response, err = c.generateOnce(ctx, req)
		response.Provider = c.Identity()
		response.ProtocolRetries = attempt
		response.ReasoningEffort = c.cfg.ReasoningEffort
		response.StreamFallback = originalStreaming
		if err == nil {
			err = validateBrowserRole(req.Role, req.Input, response.Output)
		}
		if err == nil || !errors.Is(err, ai.ErrInvalidOutput) {
			return response, err
		}
	}
	return response, err
}

// Wire-shape validation complements (and never replaces) the application checks
// for evidence, resources, agency and Canon. Flexible setup/editor roles retain
// their operation-specific application validators.
func validateBrowserRole(role string, input, output []byte) error {
	var obj map[string]any
	if json.Unmarshal(output, &obj) != nil || len(obj) == 0 {
		return fmt.Errorf("%w: empty or invalid object", ai.ErrInvalidOutput)
	}
	if role == "structured_repair" {
		var target struct {
			Role string `json:"role"`
		}
		if json.Unmarshal(input, &target) == nil {
			role = target.Role
		}
	}
	if schema := roleSchema(role); schema != nil {
		if err := checkBrowserSchema(schema, obj, 0); err != nil {
			return fmt.Errorf("%w: role %s shape", ai.ErrInvalidOutput, role)
		}
	}
	if role == "choices" {
		seen := map[string]bool{}
		for _, item := range obj["choices"].([]any) {
			id := item.(map[string]any)["id"].(string)
			if seen[id] {
				return fmt.Errorf("%w: duplicate choice id", ai.ErrInvalidOutput)
			}
			seen[id] = true
		}
	}
	return nil
}
func checkBrowserSchema(schema map[string]any, value any, depth int) error {
	invalid := errors.New("schema mismatch")
	if depth > 32 {
		return invalid
	}
	switch schema["type"] {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return invalid
		}
		props := schema["properties"].(map[string]any)
		for _, key := range schema["required"].([]string) {
			v, ok := obj[key]
			if !ok {
				return invalid
			}
			if text, ok := v.(string); ok && strings.TrimSpace(text) == "" {
				return invalid
			}
		}
		for key, v := range obj {
			p, exists := props[key]
			if !exists {
				if schema["additionalProperties"] == false {
					return invalid
				}
				continue
			}
			if err := checkBrowserSchema(p.(map[string]any), v, depth+1); err != nil {
				return err
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return invalid
		}
		if min, ok := schema["minItems"].(int); ok && len(items) < min {
			return invalid
		}
		if max, ok := schema["maxItems"].(int); ok && len(items) > max {
			return invalid
		}
		for _, v := range items {
			if err := checkBrowserSchema(schema["items"].(map[string]any), v, depth+1); err != nil {
				return err
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return invalid
		}
		if choices, ok := schema["enum"].([]string); ok {
			found := false
			for _, c := range choices {
				found = found || text == c
			}
			if !found {
				return invalid
			}
		}
	case "integer":
		n, ok := value.(float64)
		if !ok || math.Trunc(n) != n {
			return invalid
		}
		if min, ok := schema["minimum"].(int); ok && n < float64(min) {
			return invalid
		}
		if max, ok := schema["maximum"].(int); ok && n > float64(max) {
			return invalid
		}
	default:
		return invalid
	}
	return nil
}
