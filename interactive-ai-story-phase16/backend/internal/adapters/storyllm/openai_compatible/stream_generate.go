package openai_compatible

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func (c *Client) generateWriterStream(ctx context.Context, request ai.StoryRequest, system string, schema map[string]any, chat chatRequest, agent bool) (ai.StoryResponse, error) {
	result := ai.StoryResponse{Provider: c.Identity()}
	retries := 0
	schemaFallback := false
	for attempt := 0; attempt < 2; attempt++ {
		var body []byte
		var err error
		if agent {
			body, err = json.Marshal(interactionRequest{Stream: true, Agent: c.cfg.Model, Environment: "remote", Input: string(request.Input), SystemInstruction: system + " Do not use tools; answer from the supplied story context only.", ResponseFormat: interactionResponseFormat{Type: "text", MIMEType: "application/json", Schema: schema}})
		} else {
			chat.Stream = true
			chat.StreamOptions = map[string]bool{"include_usage": true}
			body, err = json.Marshal(chat)
		}
		if err != nil {
			return result, err
		}
		response, err := c.openWriterStream(ctx, body, agent)
		if errors.Is(err, errSchemaUnsupported) && schema != nil && !schemaFallback {
			schemaFallback = true
			retries++
			c.schemaUnsupported.Store(true)
			schema = nil
			chat.ResponseFormat = responseFormat{Type: "json_object"}
			continue
		}
		if errors.Is(err, errStreamUnsupported) {
			c.streamingUnsupported.Store(true)
			request.OnText = nil
			fallback, fallbackErr := c.Generate(ctx, request)
			extra := fallback.ProtocolRetries
			if extra == 0 && fallback.SchemaFallback {
				extra = 1
			}
			fallback.ProtocolRetries = extra + retries + 1
			fallback.SchemaFallback = fallback.SchemaFallback || schemaFallback
			fallback.StreamFallback = true
			return fallback, fallbackErr
		}
		if err != nil {
			return result, err
		}
		if strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
			if agent {
				result, err = c.readAgentWriterStream(ctx, response.Body, request.OnText)
			} else {
				result, err = c.readChatWriterStream(ctx, response.Body, request.OnText)
			}
		} else {
			// Some compatible endpoints ignore stream=true. Accept the single complete
			// reply without spending another request or pretending it was incremental.
			c.streamingUnsupported.Store(true)
			raw, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
			err = readErr
			if err == nil {
				var content string
				if agent {
					var decoded interactionResponse
					err = json.Unmarshal(raw, &decoded)
					if err == nil && decoded.Status != "completed" {
						err = errors.New("incomplete interaction")
					}
					content = lastModelOutputText(decoded)
					result.InputTokens, result.OutputTokens = decoded.Usage.InputTokens, decoded.Usage.OutputTokens
					result.CachedInputTokens = optionalTokenCount(decoded.Usage.CachedTokens)
				} else {
					var decoded chatResponse
					err = json.Unmarshal(raw, &decoded)
					if err == nil && len(decoded.Choices) != 1 {
						err = errors.New("invalid completion count")
					}
					if len(decoded.Choices) == 1 {
						content = decoded.Choices[0].Message.Content
					}
					result.InputTokens, result.OutputTokens = decoded.Usage.PromptTokens, decoded.Usage.CompletionTokens
					result.CachedInputTokens = reportedCachedTokens(decoded.Usage.PromptDetails)
					result.ReasoningTokens = reportedReasoningTokens(decoded.Usage.CompletionDetails)
				}
				if err == nil {
					parser := writerTextStream{}
					err = parser.feed(content, nil)
					if err == nil {
						result.Output, err = parser.finish(request.OnText)
					}
				}
			}
			result.StreamFallback = true
		}
		_ = response.Body.Close()
		result.Provider = c.Identity()
		result.SchemaFallback = schemaFallback
		result.ProtocolRetries = retries
		result.ResponseFormat = "json_object"
		if schema != nil {
			result.ResponseFormat = "json_schema"
		}
		if !agent {
			result.ReasoningEffort = chat.ReasoningEffort
		}
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, fmt.Errorf("%w: writer stream incomplete or invalid", ai.ErrInvalidOutput)
		}
		return result, nil
	}
	return result, fmt.Errorf("%w: bounded stream negotiation exhausted", ai.ErrUnavailable)
}
func (c *Client) readChatWriterStream(ctx context.Context, reader io.Reader, onText func(string)) (ai.StoryResponse, error) {
	result := ai.StoryResponse{Streamed: true}
	parser := writerTextStream{}
	done, finished := false, false
	err := consumeWriterSSE(ctx, reader, func(_ string, data []byte) error {
		if string(data) == "[DONE]" {
			done = true
			return errSSEFinished
		}
		var chunk struct {
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal(data, &chunk) != nil {
			return errors.New("invalid stream chunk")
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return errors.New("provider stream error")
		}
		if len(chunk.Choices) > 1 {
			return errors.New("multiple streaming choices")
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				return errors.New("unexpected streaming choice")
			}
			if err := parser.feed(choice.Delta.Content, onText); err != nil {
				return err
			}
			if choice.FinishReason != nil {
				if *choice.FinishReason != "stop" {
					return errors.New("writer stream stopped abnormally")
				}
				finished = true
			}
		}
		if len(chunk.Usage) > 0 && string(chunk.Usage) != "null" {
			var usage chatResponse
			if json.Unmarshal(data, &usage) != nil {
				return errors.New("invalid stream usage")
			}
			result.InputTokens, result.OutputTokens = usage.Usage.PromptTokens, usage.Usage.CompletionTokens
			result.CachedInputTokens = reportedCachedTokens(usage.Usage.PromptDetails)
			result.ReasoningTokens = reportedReasoningTokens(usage.Usage.CompletionDetails)
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if !done || !finished {
		return result, errors.New("writer stream missing terminal marker")
	}
	result.Output, err = parser.finish(onText)
	return result, err
}
func (c *Client) readAgentWriterStream(ctx context.Context, reader io.Reader, onText func(string)) (ai.StoryResponse, error) {
	result := ai.StoryResponse{Streamed: true}
	steps := map[int]string{}
	active := -1
	parser := writerTextStream{}
	complete := false
	err := consumeWriterSSE(ctx, reader, func(name string, data []byte) error {
		var event struct {
			EventType string `json:"event_type"`
			Index     int    `json:"index"`
			Step      struct {
				Type string `json:"type"`
			} `json:"step"`
			Delta       struct{ Type, Text string } `json:"delta"`
			Interaction interactionResponse         `json:"interaction"`
		}
		if json.Unmarshal(data, &event) != nil {
			return errors.New("invalid interaction event")
		}
		kind := event.EventType
		if kind == "" {
			kind = name
		}
		switch kind {
		case "step.start":
			steps[event.Index] = event.Step.Type
		case "step.delta":
			if steps[event.Index] == "model_output" && event.Delta.Type == "text" {
				if active != event.Index {
					if active >= 0 && onText != nil {
						onText("")
					}
					parser = writerTextStream{}
					active = event.Index
				}
				return parser.feed(event.Delta.Text, onText)
			}
		case "interaction.completed":
			if event.Interaction.Status != "completed" {
				return errors.New("interaction did not complete")
			}
			complete = true
			result.InputTokens, result.OutputTokens = event.Interaction.Usage.InputTokens, event.Interaction.Usage.OutputTokens
			result.CachedInputTokens = optionalTokenCount(event.Interaction.Usage.CachedTokens)
			if content := lastModelOutputText(event.Interaction); content != "" {
				parser = writerTextStream{}
				if err := parser.feed(content, nil); err != nil {
					return err
				}
			}
			return errSSEFinished
		case "error", "interaction.failed", "interaction.cancelled":
			return errors.New("interaction stream failed")
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if !complete {
		return result, errors.New("interaction stream missing completion")
	}
	result.Output, err = parser.finish(onText)
	return result, err
}
