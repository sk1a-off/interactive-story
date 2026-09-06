package contextbuilder

import (
	"context"
	"errors"
	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
	"testing"
)

type fakeMemory struct {
	got  memory.Query
	rows []memory.Memory
	err  error
}

func (f *fakeMemory) Search(_ context.Context, q memory.Query) ([]memory.Memory, error) {
	f.got = q
	return f.rows, f.err
}

func TestRetrievalFailureFallsBackToAuthoritativeAndRecentContext(t *testing.T) {
	tid := timeline.ID(iid("000000000021"))
	story := iid("000000000022")
	owner := iid("000000000023")
	b := Builder{Embeddings: fakeai.NewEmbeddingProvider(4), Memory: &fakeMemory{err: errors.New("vector store unavailable")}}
	out, err := b.Build(context.Background(), Input{State: AuthoritativeState{StoryID: story, TimelineID: tid, HeadEventSeq: 20, CurrentState: []byte(`{"state":"current"}`), RecentBeats: []string{"recent beat"}}, QueryText: "query", OwnerType: memory.OwnerCharacter, OwnerID: owner, RetrievalLimit: 8, TokenBudget: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Authoritative) != `{"state":"current"}` || len(out.RecentBeats) != 1 || out.RetrievalError == "" {
		t.Fatalf("unsafe retrieval fallback: %#v", out)
	}
}

func TestRetrievedMemoryIsDeterministicAndDeduplicated(t *testing.T) {
	tid := timeline.ID(iid("000000000031"))
	story := iid("000000000032")
	owner := iid("000000000033")
	duplicateID := iid("000000000034")
	rows := []memory.Memory{
		{ID: iid("000000000036"), StoryID: story, TimelineID: tid, OwnerType: memory.OwnerCharacter, OwnerID: owner, ValidFromSeq: 1, Content: "lower score", Score: 0.2},
		{ID: duplicateID, StoryID: story, TimelineID: tid, OwnerType: memory.OwnerCharacter, OwnerID: owner, ValidFromSeq: 3, Content: "best fact", Score: 0.9},
		{ID: duplicateID, StoryID: story, TimelineID: tid, OwnerType: memory.OwnerCharacter, OwnerID: owner, ValidFromSeq: 2, Content: "duplicate id", Score: 0.8},
		{ID: iid("000000000037"), StoryID: story, TimelineID: tid, OwnerType: memory.OwnerCharacter, OwnerID: owner, ValidFromSeq: 1, Content: "  RECENT   BEAT ", Score: 1},
	}
	b := Builder{Embeddings: fakeai.NewEmbeddingProvider(4), Memory: &fakeMemory{rows: rows}}
	out, err := b.Build(context.Background(), Input{State: AuthoritativeState{StoryID: story, TimelineID: tid, HeadEventSeq: 20, CurrentState: []byte(`{}`), RecentBeats: []string{"recent beat"}}, QueryText: "query", OwnerType: memory.OwnerCharacter, OwnerID: owner, RetrievalLimit: 8, TokenBudget: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Retrieved) != 2 || out.Retrieved[0].Content != "best fact" || out.Retrieved[1].Content != "lower score" {
		t.Fatalf("non-deterministic or duplicate retrieval: %#v", out.Retrieved)
	}
}
func iid(s string) id.ID { return id.MustParse("00000000-0000-4000-8000-" + s) }
func TestBuilderKeepsAuthoritativeStateAndFiltersLeakage(t *testing.T) {
	tid := timeline.ID(iid("000000000001"))
	story := iid("000000000002")
	owner := iid("000000000003")
	other := timeline.ID(iid("000000000004"))
	expired := int64(4)
	m := &fakeMemory{rows: []memory.Memory{
		{ID: iid("000000000010"), StoryID: story, TimelineID: tid, OwnerType: memory.OwnerCharacter, OwnerID: owner, ValidFromSeq: 1, Kind: "memory", Content: "valid"},
		{ID: iid("000000000011"), StoryID: story, TimelineID: other, OwnerType: memory.OwnerCharacter, OwnerID: owner, ValidFromSeq: 1, Content: "sibling leak"},
		{ID: iid("000000000012"), StoryID: story, TimelineID: tid, OwnerType: memory.OwnerCharacter, OwnerID: owner, ValidFromSeq: 1, ValidToSeq: &expired, Content: "expired"},
	}}
	b := Builder{Embeddings: fakeai.NewEmbeddingProvider(4), Memory: m}
	out, err := b.Build(context.Background(), Input{State: AuthoritativeState{StoryID: story, TimelineID: tid, HeadEventSeq: 5, CurrentState: []byte(`{"door":"locked"}`)}, QueryText: "door", OwnerType: memory.OwnerCharacter, OwnerID: owner, RetrievalLimit: 8, TokenBudget: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Authoritative) != `{"door":"locked"}` {
		t.Fatal("authoritative state was replaced")
	}
	if len(out.Retrieved) != 1 || out.Retrieved[0].Content != "valid" {
		t.Fatalf("retrieval leakage: %#v", out.Retrieved)
	}
	if m.got.TimelineID != tid || m.got.OwnerID != owner || m.got.AtEventSeq != 5 {
		t.Fatal("scope filters not sent to adapter")
	}
}
func TestNoQuerySkipsEmbeddingAndRetrieval(t *testing.T) {
	m := &fakeMemory{}
	b := Builder{Embeddings: fakeai.NewEmbeddingProvider(4), Memory: m}
	out, err := b.Build(context.Background(), Input{State: AuthoritativeState{CurrentState: []byte(`x`)}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Authoritative) != "x" || len(out.Retrieved) != 0 {
		t.Fatal("unexpected retrieval")
	}
}
