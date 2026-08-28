package contextbuilder

import (
	"context"
	"errors"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

var ErrEmbeddingShape = errors.New("embedding provider returned unexpected shape")

type AuthoritativeState struct {
	StoryID      id.ID
	TimelineID   timeline.ID
	HeadEventSeq int64
	CurrentState []byte
	RecentBeats  []string
	Summaries    []string
}
type Input struct {
	State          AuthoritativeState
	QueryText      string
	OwnerType      memory.OwnerType
	OwnerID        id.ID
	TokenBudget    int
	RetrievalLimit int
}
type Result struct {
	// Authoritative state is deliberately separate and first-class; retrieval
	// can supplement it but can never replace or override it.
	Authoritative []byte
	RecentBeats   []string
	Summaries     []string
	Retrieved     []memory.Memory
}
type Builder struct {
	Embeddings aiport.EmbeddingProvider
	Memory     memory.Retriever
}

func (b Builder) Build(ctx context.Context, in Input) (Result, error) {
	out := Result{Authoritative: append([]byte(nil), in.State.CurrentState...), RecentBeats: append([]string(nil), in.State.RecentBeats...), Summaries: append([]string(nil), in.State.Summaries...)}
	if in.QueryText == "" || in.RetrievalLimit <= 0 {
		return ApplyBudget(out, in.TokenBudget), nil
	}
	e, err := b.Embeddings.EmbedQueries(ctx, aiport.EmbeddingRequest{Texts: []string{in.QueryText}})
	if err != nil {
		return Result{}, err
	}
	if len(e.Vectors) != 1 || len(e.Vectors[0]) != b.Embeddings.Dimensions() {
		return Result{}, ErrEmbeddingShape
	}
	found, err := b.Memory.Search(ctx, memory.Query{StoryID: in.State.StoryID, TimelineID: in.State.TimelineID, OwnerType: in.OwnerType, OwnerID: in.OwnerID, AtEventSeq: in.State.HeadEventSeq, Vector: e.Vectors[0], Limit: in.RetrievalLimit})
	if err != nil {
		return Result{}, err
	}
	// Defensive application-layer verification prevents a buggy adapter from
	// leaking sibling/foreign/invalid memories into an LLM context.
	for _, m := range found {
		if m.StoryID != in.State.StoryID || m.TimelineID != in.State.TimelineID || m.OwnerType != in.OwnerType || m.OwnerID != in.OwnerID || m.ValidFromSeq > in.State.HeadEventSeq || (m.ValidToSeq != nil && *m.ValidToSeq < in.State.HeadEventSeq) {
			continue
		}
		out.Retrieved = append(out.Retrieved, m)
	}
	return ApplyBudget(out, in.TokenBudget), nil
}
