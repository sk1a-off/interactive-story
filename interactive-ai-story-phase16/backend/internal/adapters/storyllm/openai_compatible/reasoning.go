package openai_compatible

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Overrides are an experiment knob, not a new default or a server setting.
// No undocumented managed-agent/local-provider parameters are sent.
func validateReasoningOverrides(provider, model string, values map[string]string) error {
	for role, effort := range values {
		if provider != "google_gemini" || (model != "gemini-3.5-flash-lite" && model != "gemini-3.1-flash-lite") {
			return errors.New("reasoning override unsupported for provider/model")
		}
		switch role {
		case "action_interpreter", "turn_planner", "director", "pacing", "writer", "world_evaluator", "state_evaluator", "quest_evaluator", "choices", "structured_repair":
		default:
			return errors.New("reasoning override requires a supported turn role")
		}
		if effort != "minimal" && effort != "low" {
			return errors.New("reasoning override must be minimal or low")
		}
	}
	return nil
}

// Optional provider usage must not make otherwise valid story output unusable.
func reportedReasoningTokens(raw json.RawMessage) *int64 {
	var details struct {
		Tokens json.RawMessage `json:"reasoning_tokens"`
	}
	if json.Unmarshal(raw, &details) != nil || len(details.Tokens) == 0 || bytes.Equal(bytes.TrimSpace(details.Tokens), []byte("null")) {
		return nil
	}
	var tokens int64
	if json.Unmarshal(details.Tokens, &tokens) != nil || tokens < 0 {
		return nil
	}
	return &tokens
}
