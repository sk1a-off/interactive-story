package generationmetrics

import (
	"context"
	"errors"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

var ErrNotFound = errors.New("generation metrics not found")
var ErrUnavailable = errors.New("generation metrics unavailable")

type AttemptTiming struct {
	Attempt         int   `json:"attempt"`
	QueueWaitMS     int64 `json:"queueWaitMs"`
	WriterReadyMS   int64 `json:"writerReadyMs"`
	PipelineTotalMS int64 `json:"pipelineTotalMs"`
	Succeeded       bool  `json:"succeeded"`
}
type RoleTiming struct {
	Attempt           int    `json:"attempt"`
	Role              string `json:"role"`
	Model             string `json:"model"`
	Calls             int64  `json:"calls"`
	RecordedLatencyMS int64  `json:"recordedLatencyMs"`
	InputTokens       int64  `json:"inputTokens"`
	OutputTokens      int64  `json:"outputTokens"`
	Repairs           int64  `json:"repairs"`
	RecordedRetries   int64  `json:"recordedRetries"`
	Failures          int64  `json:"failures"`
}
type Report struct {
	GenerationID id.ID           `json:"generationId"`
	Status       string          `json:"status"`
	AttemptCount int             `json:"attemptCount"`
	Attempts     []AttemptTiming `json:"attempts"`
	Roles        []RoleTiming    `json:"roles"`
	Notes        []string        `json:"notes"`
}
type Reader interface {
	Read(context.Context, id.ID, id.ID) (Report, error)
}
