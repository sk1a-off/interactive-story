package contextbuilder

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/memory"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

type stubVisibility struct {
	plan  domain.Visibility
	err   error
	calls int
}

func (s *stubVisibility) Resolve(context.Context, id.ID, timeline.ID, int64) (domain.Visibility, error) {
	s.calls++
	return s.plan, s.err
}

type queryEmbeddings struct {
	*fakeai.EmbeddingProvider
	query func(context.Context, aiport.EmbeddingRequest) (aiport.EmbeddingResponse, error)
	texts []string
}

func (e *queryEmbeddings) EmbedQueries(ctx context.Context, r aiport.EmbeddingRequest) (aiport.EmbeddingResponse, error) {
	e.texts = append([]string(nil), r.Texts...)
	if e.query != nil {
		return e.query(ctx, r)
	}
	return e.EmbeddingProvider.EmbedQueries(ctx, r)
}
func (e *queryEmbeddings) EmbedDocuments(context.Context, aiport.EmbeddingRequest) (aiport.EmbeddingResponse, error) {
	panic("Context Builder must use query embeddings")
}

func branchFixture(t *testing.T) (Builder, Input, *fakeMemory, *queryEmbeddings, *stubVisibility, timeline.ID) {
	t.Helper()
	story := iid("000000000101")
	root := timeline.ID(iid("000000000102"))
	child := timeline.ID(iid("000000000103"))
	plan, err := domain.ResolveVisibility(story, child, 12, []domain.LineageNode{
		{StoryID: story, TimelineID: root, HeadEventSeq: 20},
		{StoryID: story, TimelineID: child, ParentTimelineID: &root, ForkEventSeq: 10, HeadEventSeq: 15},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver := &stubVisibility{plan: plan}
	embeddings := &queryEmbeddings{EmbeddingProvider: fakeai.NewEmbeddingProvider(384)}
	store := &fakeMemory{}
	profile := memory.EmbeddingProfile{Provider: embeddings.ID.Provider, Model: embeddings.ID.Model, Revision: 1}
	b := Builder{Embeddings: embeddings, Memory: store, Profile: profile, Visibility: resolver}
	in := Input{State: AuthoritativeState{StoryID: story, TimelineID: child, HeadEventSeq: 12, CurrentState: []byte(`{"door":"locked"}`), RecentBeats: []string{"recent beat"}, Summaries: []string{"summary"}},
		QueryText: "действие игрока и цель директора", OwnerType: memory.OwnerTimeline, OwnerID: id.ID(child), RetrievalLimit: 8, TokenBudget: 5000}
	return b, in, store, embeddings, resolver, root
}

func branchRow(b Builder, in Input, source timeline.ID, key string, from int64) memory.Memory {
	rowID, _ := id.New()
	return memory.Memory{ID: rowID, StoryID: in.State.StoryID, TimelineID: source, OwnerType: in.OwnerType, OwnerID: id.ID(source),
		ValidFromSeq: from, DocumentClass: "historical_beat", SourceKey: key, Profile: b.Profile, Kind: "beat", Content: key, Score: 1}
}

func TestBranchBuilderChecksOriginalSourceAndIndependentVisibility(t *testing.T) {
	b, in, store, embeddings, _, root := branchFixture(t)
	ancestor := branchRow(b, in, root, "ancestor", 10)
	cap := int64(10)
	ancestor.ValidToSeq = &cap // expires before child head, but valid at the source cap
	local := branchRow(b, in, in.State.TimelineID, "local", 10)
	local.DocumentClass = "branch_state"
	store.rows = []memory.Memory{ancestor, local}
	for name, change := range map[string]func(*memory.Memory){
		"foreign story": func(m *memory.Memory) { m.StoryID = iid("000000000199") },
		"sibling": func(m *memory.Memory) {
			m.TimelineID = timeline.ID(iid("000000000198"))
			m.OwnerID = id.ID(m.TimelineID)
		},
		"ancestor future":         func(m *memory.Memory) { m.ValidFromSeq = 11; m.ValidToSeq = nil },
		"ancestor expired":        func(m *memory.Memory) { end := int64(9); m.ValidToSeq = &end },
		"parent structured state": func(m *memory.Memory) { m.DocumentClass = "branch_state" },
		"relabeled owner":         func(m *memory.Memory) { m.OwnerID = in.OwnerID },
		"another owner":           func(m *memory.Memory) { m.OwnerType = memory.OwnerCharacter },
		"profile":                 func(m *memory.Memory) { m.Profile.Revision++ },
		"provider":                func(m *memory.Memory) { m.Profile.Provider = "other" },
		"model":                   func(m *memory.Memory) { m.Profile.Model = "other" },
		"unclassified":            func(m *memory.Memory) { m.DocumentClass = "" },
		"missing source":          func(m *memory.Memory) { m.SourceKey = "" },
		"missing id":              func(m *memory.Memory) { m.ID = "" },
		"nan":                     func(m *memory.Memory) { m.Score = math.NaN() },
		"infinity":                func(m *memory.Memory) { m.Score = math.Inf(1) },
	} {
		row := branchRow(b, in, root, name, 10)
		row.Score = 100
		change(&row)
		store.rows = append(store.rows, row)
	}
	for name, from := range map[string]int64{"before branch baseline": 9, "local future": 13} {
		row := branchRow(b, in, in.State.TimelineID, name, from)
		row.DocumentClass = "branch_state"
		store.rows = append(store.rows, row)
	}
	duplicate := local
	duplicate.ID, _ = id.New()
	duplicate.Content = "same source duplicate"
	duplicate.Score = .5
	store.rows = append(store.rows, duplicate)
	out, err := b.Build(context.Background(), in)
	if err != nil || out.RetrievalError != "" || len(out.Retrieved) != 2 {
		t.Fatalf("result=%+v err=%v", out, err)
	}
	for _, row := range out.Retrieved {
		if row.SourceKey != "ancestor" && row.SourceKey != "local" {
			t.Fatalf("leaked: %+v", row)
		}
	}
	if store.got.Profile != b.Profile || store.got.Text != in.QueryText || !reflect.DeepEqual(embeddings.texts, []string{in.QueryText}) {
		t.Fatal("lost query text/embedding identity")
	}
	if string(out.Authoritative) != string(in.State.CurrentState) || !reflect.DeepEqual(out.RecentBeats, in.State.RecentBeats) {
		t.Fatal("authoritative context changed")
	}
}

func TestBranchBuilderKeepsCharacterKnowledgePrivate(t *testing.T) {
	b, in, store, _, _, root := branchFixture(t)
	in.OwnerType = memory.OwnerCharacter
	in.OwnerID = iid("000000000111")
	local := branchRow(b, in, in.State.TimelineID, "knowledge", 10)
	local.DocumentClass = "branch_state"
	local.OwnerID = in.OwnerID
	local.Kind = "knowledge"
	parent := local
	parent.ID, _ = id.New()
	parent.TimelineID = root
	parent.SourceKey = "parent knowledge"
	other := local
	other.ID, _ = id.New()
	other.OwnerID = iid("000000000112")
	other.SourceKey = "other character"
	store.rows = []memory.Memory{parent, other, local}
	out, err := b.Build(context.Background(), in)
	if err != nil || len(out.Retrieved) != 1 || out.Retrieved[0].ID != local.ID || out.Retrieved[0].Kind != "knowledge" {
		t.Fatalf("private memory leak: %+v %v", out, err)
	}
}

func TestBranchBuilderFailuresKeepFallbackAndHideProviderDetails(t *testing.T) {
	for _, name := range []string{"visibility", "wrong plan", "profile", "dimensions", "embedding", "shape", "zero", "nan", "infinity", "search"} {
		t.Run(name, func(t *testing.T) {
			b, in, store, e, resolver, _ := branchFixture(t)
			switch name {
			case "visibility":
				resolver.err = errors.New("secret provider credential")
			case "wrong plan":
				resolver.plan = domain.Visibility{}
			case "profile":
				b.Profile.Model = "other"
			case "dimensions":
				e.Dims = 4
			case "embedding":
				e.query = func(context.Context, aiport.EmbeddingRequest) (aiport.EmbeddingResponse, error) {
					return aiport.EmbeddingResponse{}, errors.New("secret provider credential")
				}
			case "search":
				store.err = errors.New("secret provider credential")
			default:
				e.query = func(context.Context, aiport.EmbeddingRequest) (aiport.EmbeddingResponse, error) {
					if name == "shape" {
						return aiport.EmbeddingResponse{}, nil
					}
					v := make([]float32, 384)
					if name == "nan" {
						v[0] = float32(math.NaN())
					}
					if name == "infinity" {
						v[0] = float32(math.Inf(1))
					}
					return aiport.EmbeddingResponse{Vectors: [][]float32{v}}, nil
				}
			}
			out, err := b.Build(context.Background(), in)
			if err != nil || out.RetrievalError == "" || out.RetrievalError == "secret provider credential" || len(out.Retrieved) != 0 {
				t.Fatalf("fallback: %+v %v", out, err)
			}
			if !reflect.DeepEqual(out.Authoritative, in.State.CurrentState) || !reflect.DeepEqual(out.RecentBeats, in.State.RecentBeats) || !reflect.DeepEqual(out.Summaries, in.State.Summaries) {
				t.Fatal("fallback lost reserved state")
			}
			if name != "search" && store.got.Limit != 0 {
				t.Fatal("search invoked after earlier failure")
			}
		})
	}
}

func TestBranchBuilderDeadlineAndZeroMemoryBudget(t *testing.T) {
	b, in, store, e, resolver, _ := branchFixture(t)
	zero := 0
	in.MemoryTokenBudget = &zero
	if _, err := b.Build(context.Background(), in); err != nil || resolver.calls != 0 || e.Calls != 0 {
		t.Fatal("zero budget invoked dependencies")
	}
	in.MemoryTokenBudget = nil
	in.TokenBudget = 1
	if _, err := b.Build(context.Background(), in); err != nil || resolver.calls != 0 || e.Calls != 0 {
		t.Fatal("exhausted context invoked dependencies")
	}
	in.TokenBudget = 5000
	in.MemoryTokenBudget = nil
	b.RetrievalTimeout = 10 * time.Millisecond
	e.query = func(ctx context.Context, _ aiport.EmbeddingRequest) (aiport.EmbeddingResponse, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 100*time.Millisecond {
			t.Error("missing retrieval deadline")
		}
		<-ctx.Done()
		return aiport.EmbeddingResponse{}, ctx.Err()
	}
	out, err := b.Build(context.Background(), in)
	if err != nil || out.RetrievalError == "" || store.got.Limit != 0 || len(out.RecentBeats) != 1 {
		t.Fatalf("timeout fallback: %+v %v", out, err)
	}
}

func TestMemoryBudgetIncludesProvenanceAndReservesRecentContext(t *testing.T) {
	b, in, _, _, _, root := branchFixture(t)
	row := branchRow(b, in, root, "retrieved", 10)
	raw, _ := json.Marshal(row)
	cost := estimate(string(raw))
	base := Result{Authoritative: in.State.CurrentState, RecentBeats: in.State.RecentBeats, Summaries: in.State.Summaries, Retrieved: []memory.Memory{row, row}}
	reserved := estimate(string(base.Authoritative)) + estimate(base.RecentBeats[0]) + estimate(base.Summaries[0])
	for _, test := range []struct{ total, cap, count int }{
		{5000, cost - 1, 0}, {5000, cost, 1}, {5000, 2 * cost, 2}, {reserved + cost - 1, 2 * cost, 0}, {1, 5000, 0}, {5000, 0, 0},
	} {
		out := ApplyBudgetWithMemoryLimit(base, test.total, test.cap)
		if len(out.Retrieved) != test.count || string(out.Authoritative) != string(base.Authoritative) {
			t.Fatalf("budget %+v: %+v", test, out)
		}
		if test.total >= reserved && (!reflect.DeepEqual(out.RecentBeats, base.RecentBeats) || !reflect.DeepEqual(out.Summaries, base.Summaries)) {
			t.Fatal("retrieval displaced reserved context")
		}
	}
}

func TestBranchBuilderDefaultMemoryBudgetAndLimit(t *testing.T) {
	b, in, store, _, _, root := branchFixture(t)
	large := branchRow(b, in, root, "large", 10)
	large.Content = strings.Repeat("д", 5000)
	store.rows = []memory.Memory{large}
	out, err := b.Build(context.Background(), in)
	if err != nil || len(out.Retrieved) != 0 || len(out.RecentBeats) != 1 {
		t.Fatalf("default memory cap: %+v %v", out, err)
	}
	store.rows = nil
	for _, key := range []string{"one", "two", "three"} {
		store.rows = append(store.rows, branchRow(b, in, root, key, 10))
	}
	in.RetrievalLimit = 1
	out, err = b.Build(context.Background(), in)
	if err != nil || len(out.Retrieved) != 1 {
		t.Fatalf("adapter exceeded top-k: %+v %v", out, err)
	}
	older := branchRow(b, in, root, "older", 1)
	older.ID = iid("000000000121")
	newer := branchRow(b, in, root, "newer", 10)
	newer.ID = iid("000000000122")
	store.rows = []memory.Memory{newer, older}
	out, err = b.Build(context.Background(), in)
	if err != nil || len(out.Retrieved) != 1 || out.Retrieved[0].ID != older.ID {
		t.Fatal("builder changed hybrid UUID tie order")
	}
}
