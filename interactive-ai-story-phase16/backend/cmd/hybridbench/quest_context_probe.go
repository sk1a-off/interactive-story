package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

// This experiment is deliberately confined to the benchmark, not the server.
// It removes background prose, not current balances, rules, objective history,
// the immediately previous beat or the player's constraints. Unknown fields stay.
func compactQuestInput(raw json.RawMessage) (json.RawMessage, error) {
	var input map[string]json.RawMessage
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(input["context"], &state); err != nil {
		return nil, err
	}
	if state == nil {
		return nil, fmt.Errorf("missing context")
	}
	for _, key := range []string{"visualBible", "initialCast", "recentBeats"} {
		delete(state, key)
	}
	var err error
	input["context"], err = json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return json.Marshal(input)
}

type questContextProbe struct {
	Variant              string          `json:"variant"`
	BaselineInputSHA256  string          `json:"baselineInputSha256"`
	CandidateInputSHA256 string          `json:"candidateInputSha256"`
	InputBytes           int             `json:"inputBytes"`
	ElapsedMS            int64           `json:"elapsedMs"`
	InputTokens          int64           `json:"inputTokens"`
	OutputTokens         int64           `json:"outputTokens"`
	Output               json.RawMessage `json:"output,omitempty"`
	Error                string          `json:"error,omitempty"`
	SameJSON             bool            `json:"sameJson"`
	ChangesArray         bool            `json:"changesArray"`
}

func probeQuestContext(ctx context.Context, client ai.StoryLLM, request ai.StoryRequest, baseline ai.StoryResponse) *questContextProbe {
	p := &questContextProbe{Variant: "quest-pruned-v1", BaselineInputSHA256: fmt.Sprintf("%x", sha256.Sum256(request.Input))}
	raw, err := compactQuestInput(request.Input)
	if err != nil {
		p.Error = err.Error()
		return p
	}
	p.InputBytes = len(raw)
	p.CandidateInputSHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
	request.Input = raw
	started := time.Now()
	response, err := client.Generate(ctx, request)
	p.ElapsedMS = time.Since(started).Milliseconds()
	p.InputTokens, p.OutputTokens = response.InputTokens, response.OutputTokens
	if err != nil {
		p.Error = err.Error()
		return p
	}
	if !json.Valid(response.Output) {
		p.Error = "invalid candidate JSON"
		return p
	}
	p.Output = response.Output
	var a, b any
	json.Unmarshal(baseline.Output, &a)
	json.Unmarshal(response.Output, &b)
	p.SameJSON = reflect.DeepEqual(a, b)
	if object, ok := b.(map[string]any); ok {
		_, p.ChangesArray = object["changes"].([]any)
	}
	// Neither valid JSON nor equality is a semantic quality verdict.
	return p
}
