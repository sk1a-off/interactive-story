package contextbuilder

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/memory"
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
	// Nil keeps the legacy budget; branch-aware retrieval defaults to 1024.
	// Zero disables retrieval without invoking dependencies.
	MemoryTokenBudget *int
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
	Visibility memory.VisibilityResolver
	Profile    memory.EmbeddingProfile
	// Branch-aware retrieval defaults to a shared two-second deadline.
	RetrievalTimeout time.Duration
}

func (b Builder) Build(ctx context.Context, in Input) (Result, error) {
	out := Result{Authoritative: append([]byte(nil), in.State.CurrentState...), RecentBeats: append([]string(nil), in.State.RecentBeats...), Summaries: append([]string(nil), in.State.Summaries...)}
	branchAware := b.Visibility != nil || b.Profile != (memory.EmbeddingProfile{})
	finish := func() (Result, error) {
		if in.MemoryTokenBudget != nil {
			return ApplyBudgetWithMemoryLimit(out, in.TokenBudget, *in.MemoryTokenBudget), nil
		}
		if branchAware {
			return ApplyBudgetWithMemoryLimit(out, in.TokenBudget, 1024), nil
		}
		return ApplyBudget(out, in.TokenBudget), nil
	}
	if strings.TrimSpace(in.QueryText) == "" || in.RetrievalLimit <= 0 || (in.MemoryTokenBudget != nil && *in.MemoryTokenBudget <= 0) ||
		(branchAware && estimate(string(out.Authoritative)) >= in.TokenBudget) {
		return finish()
	}
	if b.Embeddings == nil || b.Memory == nil {
		out.RetrievalError = "retrieval dependencies unavailable"
		return finish()
	}
	var plan domain.Visibility
	if branchAware {
		identity := b.Embeddings.Identity()
		if b.Visibility == nil || strings.TrimSpace(b.Profile.Provider) == "" || strings.TrimSpace(b.Profile.Model) == "" || b.Profile.Revision < 1 ||
			identity.Kind != aiport.KindEmbedding || identity.Provider != b.Profile.Provider || identity.Model != b.Profile.Model || b.Embeddings.Dimensions() != 384 {
			out.RetrievalError = "memory embedding configuration mismatch"
			return finish()
		}
		timeout := b.RetrievalTimeout
		if timeout <= 0 {
			timeout = 2 * time.Second
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
		var err error
		plan, err = b.Visibility.Resolve(ctx, in.State.StoryID, in.State.TimelineID, in.State.HeadEventSeq)
		if err != nil || !plan.Matches(in.State.StoryID, in.State.TimelineID, in.State.HeadEventSeq) {
			out.RetrievalError = "memory visibility unavailable"
			return finish()
		}
	}
	e, err := b.Embeddings.EmbedQueries(ctx, aiport.EmbeddingRequest{Texts: []string{in.QueryText}})
	if err != nil {
		out.RetrievalError = "memory query embedding unavailable"
		return finish()
	}
	if len(e.Vectors) != 1 || len(e.Vectors[0]) != b.Embeddings.Dimensions() || (branchAware && !validQueryVector(e.Vectors[0])) {
		out.RetrievalError = ErrEmbeddingShape.Error()
		return finish()
	}
	if ctx.Err() != nil {
		out.RetrievalError = "memory retrieval cancelled"
		return finish()
	}
	found, err := b.Memory.Search(ctx, memory.Query{StoryID: in.State.StoryID, TimelineID: in.State.TimelineID, OwnerType: in.OwnerType, OwnerID: in.OwnerID, AtEventSeq: in.State.HeadEventSeq, Vector: e.Vectors[0], Limit: in.RetrievalLimit, Text: in.QueryText, Profile: b.Profile})
	if err != nil {
		out.RetrievalError = "memory search unavailable"
		return finish()
	}
	if ctx.Err() != nil {
		out.RetrievalError = "memory retrieval cancelled"
		return finish()
	}
	// Defensive application-layer verification prevents a buggy adapter from
	// leaking sibling/foreign/invalid memories into an LLM context.
	eligible := make([]memory.Memory, 0, len(found))
	for _, item := range found {
		if branchAware && !allowsBranchMemory(plan, b.Profile, in, item) {
			continue
		}
		eligible = append(eligible, item)
	}
	found = eligible
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Score != found[j].Score {
			return found[i].Score > found[j].Score
		}
		if !branchAware && found[i].ValidFromSeq != found[j].ValidFromSeq {
			return found[i].ValidFromSeq > found[j].ValidFromSeq
		}
		return found[i].ID.String() < found[j].ID.String()
	})
	seenIDs := map[id.ID]bool{}
	seenContent := map[string]bool{}
	seenSources := map[string]bool{}
	for _, text := range append(append([]string(nil), in.State.RecentBeats...), in.State.Summaries...) {
		if key := normalizedContent(text); key != "" {
			seenContent[key] = true
		}
	}
	for _, m := range found {
		if !branchAware && (m.StoryID != in.State.StoryID || m.TimelineID != in.State.TimelineID || m.OwnerType != in.OwnerType || m.OwnerID != in.OwnerID || m.ValidFromSeq > in.State.HeadEventSeq || (m.ValidToSeq != nil && *m.ValidToSeq < in.State.HeadEventSeq)) {
			continue
		}
		sourceKey := string(m.TimelineID) + "/" + string(m.OwnerType) + "/" + m.OwnerID.String() + "/" + m.SourceKey
		if branchAware && seenSources[sourceKey] {
			continue
		}
		contentKey := normalizedContent(m.Content)
		if m.ID.IsZero() || seenIDs[m.ID] || contentKey == "" || seenContent[contentKey] {
			continue
		}
		seenIDs[m.ID], seenContent[contentKey] = true, true
		seenSources[sourceKey] = true
		out.Retrieved = append(out.Retrieved, m)
		if len(out.Retrieved) >= in.RetrievalLimit {
			break
		}
	}
	return finish()
}

func normalizedContent(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(value)), " "))
}
