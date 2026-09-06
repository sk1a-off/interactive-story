package contextbuilder

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
	"golang.org/x/text/unicode/norm"
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
	Authoritative  []byte
	RecentBeats    []string
	Summaries      []string
	Retrieved      []memory.Memory
	RetrievalError string
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
	if b.Embeddings == nil || b.Memory == nil {
		out.RetrievalError = "retrieval dependencies unavailable"
		return ApplyBudget(out, in.TokenBudget), nil
	}
	e, err := b.Embeddings.EmbedQueries(ctx, aiport.EmbeddingRequest{Texts: []string{in.QueryText}})
	if err != nil {
		out.RetrievalError = err.Error()
		return ApplyBudget(out, in.TokenBudget), nil
	}
	if len(e.Vectors) != 1 || len(e.Vectors[0]) != b.Embeddings.Dimensions() {
		out.RetrievalError = ErrEmbeddingShape.Error()
		return ApplyBudget(out, in.TokenBudget), nil
	}
	found, err := b.Memory.Search(ctx, memory.Query{StoryID: in.State.StoryID, TimelineID: in.State.TimelineID, OwnerType: in.OwnerType, OwnerID: in.OwnerID, AtEventSeq: in.State.HeadEventSeq, Vector: e.Vectors[0], Limit: in.RetrievalLimit})
	if err != nil {
		out.RetrievalError = err.Error()
		return ApplyBudget(out, in.TokenBudget), nil
	}
	// Defensive application-layer verification prevents a buggy adapter from
	// leaking sibling/foreign/invalid memories into an LLM context.
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Score != found[j].Score {
			return found[i].Score > found[j].Score
		}
		if found[i].ValidFromSeq != found[j].ValidFromSeq {
			return found[i].ValidFromSeq > found[j].ValidFromSeq
		}
		return found[i].ID.String() < found[j].ID.String()
	})
	seenIDs := map[id.ID]bool{}
	seenContent := map[string]bool{}
	for _, text := range append(append([]string(nil), in.State.RecentBeats...), in.State.Summaries...) {
		if key := normalizedContent(text); key != "" {
			seenContent[key] = true
		}
	}
	for _, m := range found {
		if m.StoryID != in.State.StoryID || m.TimelineID != in.State.TimelineID || m.OwnerType != in.OwnerType || m.OwnerID != in.OwnerID || m.ValidFromSeq > in.State.HeadEventSeq || (m.ValidToSeq != nil && *m.ValidToSeq < in.State.HeadEventSeq) {
			continue
		}
		contentKey := normalizedContent(m.Content)
		if m.ID.IsZero() || seenIDs[m.ID] || contentKey == "" || seenContent[contentKey] {
			continue
		}
		seenIDs[m.ID], seenContent[contentKey] = true, true
		out.Retrieved = append(out.Retrieved, m)
	}
	return ApplyBudget(out, in.TokenBudget), nil
}

func normalizedContent(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(value)), " "))
}
