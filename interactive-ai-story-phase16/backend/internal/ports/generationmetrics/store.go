package generationmetrics

import (
	"context"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

// Metrics contains non-overlapping wall-clock stages for one durable attempt.
// QueueWaitMS is outside TotalMS; the remaining stages plus UnattributedMS add
// up to TotalMS (within millisecond rounding).
type Metrics struct {
	GenerationJobID          id.ID  `json:"generationJobId,omitempty"`
	AttemptNo                int    `json:"attemptNo,omitempty"`
	ConfigRevisionID         id.ID  `json:"configRevisionId,omitempty"`
	PromptSetRevisionID      id.ID  `json:"promptSetRevisionId,omitempty"`
	ContextMode              string `json:"contextMode,omitempty"`
	QueueWaitMS              int64  `json:"queueWaitMs"`
	TargetLoadMS             int64  `json:"targetLoadMs"`
	ContextBuildMS           int64  `json:"contextBuildMs"`
	ContextShadowMS          int64  `json:"contextShadowMs"`
	ShadowRetrievedCount     int    `json:"shadowRetrievedCount"`
	ShadowRetrievalError     string `json:"shadowRetrievalError,omitempty"`
	ChoicesFallbackUsed      bool   `json:"choicesFallbackUsed"`
	ActionIntentFallbackUsed bool   `json:"actionIntentFallbackUsed"`
	PlannerMS                int64  `json:"plannerMs"`
	WriterMS                 int64  `json:"writerMs"`
	PostWriterMS             int64  `json:"postWriterMs"`
	ValidationMS             int64  `json:"validationMs"`
	CommitMS                 int64  `json:"commitMs"`
	UnattributedMS           int64  `json:"unattributedMs"`
	TotalMS                  int64  `json:"totalMs"`
	TimeToFirstStoryTextMS   int64  `json:"timeToFirstStoryTextMs"`
	Succeeded                bool   `json:"succeeded"`
	ErrorText                string `json:"error,omitempty"`
}

type Store interface {
	Record(context.Context, Metrics) error
}
