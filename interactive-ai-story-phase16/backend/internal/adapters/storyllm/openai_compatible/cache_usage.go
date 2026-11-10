package openai_compatible

import (
	"bytes"
	"encoding/json"
)

func optionalTokenCount(raw json.RawMessage) *int64 {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var tokens int64
	if json.Unmarshal(raw, &tokens) != nil || tokens < 0 {
		return nil
	}
	return &tokens
}
func reportedCachedTokens(raw json.RawMessage) *int64 {
	var details struct {
		Cached json.RawMessage `json:"cached_tokens"`
	}
	if json.Unmarshal(raw, &details) != nil {
		return nil
	}
	return optionalTokenCount(details.Cached)
}
