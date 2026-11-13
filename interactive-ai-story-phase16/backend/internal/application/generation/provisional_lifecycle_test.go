package generation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

// Full provisional-text lifecycle across a failed first attempt and a
// successful retry, as seen through the attempt-aware sink: the reader must
// be able to discard the first attempt's draft and accept the second one.
func TestProvisionalBeatLifecycleAcrossRetry(t *testing.T) {
	timelineID := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000881"))
	generationID := id.MustParse("00000000-0000-4000-8000-000000000880")
	captured := &capturedSink{}

	// Attempt 1: writer succeeds (draft published), an evaluator fails.
	first := scripted()
	first.ErrByRole["quest_evaluator"] = errors.New("provider stalled")
	p := Pipeline{LLM: first, Canon: &appender{}, Targets: targetSource{}, ProvisionalBeat: true,
		Sink: attemptAwareSink{Sink: captured, retryRemaining: true, revisionBase: 1 * 1000000}}
	if _, err := p.Run(context.Background(), generationID, PlayerAction{TimelineID: timelineID, Text: "Осмотреться"}); err == nil {
		t.Fatal("first attempt should fail")
	}
	var firstDraft, retrying Update
	for _, u := range captured.updates {
		switch u.Phase {
		case PhaseDraftReady:
			firstDraft = u
		case PhaseRetrying:
			retrying = u
		case PhaseFailed:
			t.Fatalf("non-final attempt must publish retrying, not failed: %+v", u)
		}
	}
	if !firstDraft.Provisional || firstDraft.Revision != 1000001 || strings.TrimSpace(firstDraft.TextDelta) == "" {
		t.Fatalf("attempt 1 draft must carry its epoch revision: %+v", firstDraft)
	}
	if retrying.Revision != 1999999 || retrying.Revision <= firstDraft.Revision {
		t.Fatalf("retry barrier must supersede the attempt 1 draft: %+v", retrying)
	}

	// Attempt 2: everything succeeds in a later epoch.
	second := scripted()
	canon := &appender{}
	p = Pipeline{LLM: second, Canon: canon, Targets: targetSource{}, ProvisionalBeat: true,
		Sink: attemptAwareSink{Sink: captured, retryRemaining: false, revisionBase: 2 * 1000000}}
	events, err := p.Run(context.Background(), generationID, PlayerAction{TimelineID: timelineID, Text: "Осмотреться"})
	if err != nil {
		t.Fatal(err)
	}
	var secondDraft Update
	completed := false
	for _, u := range captured.updates {
		if u.Phase == PhaseDraftReady && u.Revision > retrying.Revision {
			secondDraft = u
		}
		if u.Phase == PhaseCompleted {
			completed = true
		}
	}
	if !secondDraft.Provisional || secondDraft.Revision != 2000001 {
		t.Fatalf("attempt 2 draft must be accepted after the barrier: %+v", secondDraft)
	}
	if !completed {
		t.Fatal("completed update missing")
	}
	beats := 0
	for _, stored := range events {
		if stored.Type == "beat_committed" {
			beats++
		}
	}
	if beats != 1 {
		t.Fatalf("exactly one beat must reach Canon, got %d", beats)
	}
}

// Cancellation during evaluation must end with a failed/cancelled frame and
// no Canon commit, even though a provisional draft was already published.
func TestProvisionalBeatCancelledAfterDraftCommitsNothing(t *testing.T) {
	timelineID := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000891"))
	ctx, cancel := context.WithCancel(context.Background())
	llm := &cancelAfterWriterLLM{ScriptedStoryLLM: scripted(), cancel: cancel}
	canon := &appender{}
	sink := &memSink{}
	p := Pipeline{LLM: llm, Canon: canon, Targets: targetSource{}, ProvisionalBeat: true, Sink: sink}
	_, err := p.Run(ctx, id.MustParse("00000000-0000-4000-8000-000000000890"), PlayerAction{TimelineID: timelineID, Text: "Осмотреться"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if len(canon.events) != 0 {
		t.Fatalf("cancelled turn must not commit: %v", canon.events)
	}
	sawDraft, sawFailed := false, false
	for _, u := range sink.updates {
		sawDraft = sawDraft || u.Phase == PhaseDraftReady
		sawFailed = sawFailed || u.Phase == PhaseFailed
	}
	if !sawDraft || !sawFailed {
		t.Fatalf("expected draft then failed frames, got %v", sink.phases)
	}
}

// cancelAfterWriterLLM cancels the run context once the writer has produced
// its draft, imitating a user pressing Cancel while evaluators are running.
type cancelAfterWriterLLM struct {
	*fakeai.ScriptedStoryLLM
	cancel context.CancelFunc
}

func (l *cancelAfterWriterLLM) Generate(ctx context.Context, r ai.StoryRequest) (ai.StoryResponse, error) {
	response, err := l.ScriptedStoryLLM.Generate(ctx, r)
	if r.Role == "writer" && err == nil {
		l.cancel()
	}
	return response, err
}
