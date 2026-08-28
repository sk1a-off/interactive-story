package contextbuilder

import (
	"context"
	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
	"testing"
)

type fakeMemory struct {
	got  memory.Query
	rows []memory.Memory
}

func (f *fakeMemory) Search(_ context.Context, q memory.Query) ([]memory.Memory, error) {
	f.got = q
	return f.rows, nil
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
