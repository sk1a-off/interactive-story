package generation

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

var ErrInvalidTransition = errors.New("invalid generation job transition")

type Status string

const (
	Queued    Status = "queued"
	Running   Status = "running"
	Completed Status = "completed"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

type Job struct {
	ID, StoryID, TimelineID id.ID
	ExpectedHeadEventSeq    int64
	ConfigRevisionID        id.ID
	PromptSetRevisionID     id.ID
	Status                  Status
	Provider                aiport.ProviderIdentity
	RequestID               string
	CreatedAt               time.Time
	RequestPayload          json.RawMessage
}

func NewJob(jobID, storyID, timelineID id.ID, expectedHead int64, cfg aiconfig.Revision, kind aiport.ProviderKind, requestID string) (Job, error) {
	return NewJobWithPrompt(jobID, storyID, timelineID, expectedHead, cfg, "", kind, requestID)
}

func NewJobWithPrompt(jobID, storyID, timelineID id.ID, expectedHead int64, cfg aiconfig.Revision, promptSetRevisionID id.ID, kind aiport.ProviderKind, requestID string) (Job, error) {
	var p aiport.ProviderIdentity
	switch kind {
	case aiport.KindStoryLLM:
		p = cfg.StoryLLM
	case aiport.KindEmbedding:
		p = cfg.Embedding
	case aiport.KindImage:
		p = cfg.Image
	default:
		return Job{}, ErrInvalidTransition
	}
	return Job{ID: jobID, StoryID: storyID, TimelineID: timelineID, ExpectedHeadEventSeq: expectedHead, ConfigRevisionID: cfg.ID, PromptSetRevisionID: promptSetRevisionID, Status: Queued, Provider: p, RequestID: requestID, CreatedAt: time.Now().UTC()}, nil
}
func (j *Job) Start() error {
	if j.Status != Queued {
		return ErrInvalidTransition
	}
	j.Status = Running
	return nil
}
func (j *Job) Complete() error {
	if j.Status != Running {
		return ErrInvalidTransition
	}
	j.Status = Completed
	return nil
}
func (j *Job) Fail() error {
	if j.Status != Running && j.Status != Queued {
		return ErrInvalidTransition
	}
	j.Status = Failed
	return nil
}
func (j *Job) Cancel() error {
	if j.Status != Queued && j.Status != Running {
		return ErrInvalidTransition
	}
	j.Status = Cancelled
	return nil
}
