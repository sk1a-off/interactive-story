package generation

import (
	"context"
	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"testing"
)

func TestDuplicateSubmissionReturnsSameGeneration(t *testing.T) {
	h := NewHub()
	r := NewRunner(Pipeline{LLM: scripted(), Canon: &appender{}, Targets: targetSource{}}, h)
	a := PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000077")), Text: "x"}
	x, e := r.Submit(context.Background(), "same-request", a)
	if e != nil {
		t.Fatal(e)
	}
	y, e := r.Submit(context.Background(), "same-request", a)
	if e != nil {
		t.Fatal(e)
	}
	if x != y {
		t.Fatal("duplicate request created a second generation")
	}
}

var _ = fakeai.NewStoryLLM

func TestRunnerCapturesFactoryPerSubmission(t *testing.T) {
	h := NewHub()
	calls := 0
	factory := func(context.Context) (Pipeline, error) {
		calls++
		return Pipeline{LLM: scripted(), Canon: &appender{}, Targets: targetSource{}}, nil
	}
	r := NewRunnerWithFactory(factory, h)
	a := PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000088")), Text: "x"}
	if _, e := r.Submit(context.Background(), "one", a); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Submit(context.Background(), "two", a); e != nil {
		t.Fatal(e)
	}
	if calls != 2 {
		t.Fatalf("want provider snapshot for each new submission, got %d", calls)
	}
	if _, e := r.Submit(context.Background(), "two", a); e != nil {
		t.Fatal(e)
	}
	if calls != 2 {
		t.Fatal("duplicate submission re-resolved provider instead of retaining generation")
	}
}
