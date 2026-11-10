package generation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

type pipelineStreamFake struct {
	ai.StoryLLM
	started   chan struct{}
	release   chan struct{}
	fail      bool
	callbacks int
}

func (f *pipelineStreamFake) Generate(ctx context.Context, r ai.StoryRequest) (ai.StoryResponse, error) {
	if r.Role == "writer" && r.OnText != nil {
		f.callbacks++
		r.OnText("Первый фрагмент черновика")
		if f.started != nil {
			close(f.started)
			select {
			case <-f.release:
			case <-ctx.Done():
				return ai.StoryResponse{}, ctx.Err()
			}
		}
		if f.fail {
			return ai.StoryResponse{}, errors.New("broken stream")
		}
	}
	return f.StoryLLM.Generate(ctx, r)
}
func TestPipelineStreamShowsDraftBeforeWriterFinishesAndCommitsOnlyFinal(t *testing.T) {
	model := &pipelineStreamFake{StoryLLM: scripted(), started: make(chan struct{}), release: make(chan struct{})}
	sink := &memSink{}
	canon := &appender{}
	pipeline := Pipeline{LLM: model, Canon: canon, Targets: targetSource{}, Sink: sink, StreamWriter: true}
	result := make(chan error, 1)
	go func() { _, err := pipeline.Run(context.Background(), mustID(1), scheduleAction()); result <- err }()
	select {
	case <-model.started:
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	if len(canon.events) != 0 {
		t.Fatal("partial draft reached Canon")
	}
	found := false
	for _, u := range sink.updates {
		if u.Phase == PhaseDraftStreaming && u.TextDelta != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("draft hidden until writer completion")
	}
	close(model.release)
	if err := awaitScheduleResult(t, result); err != nil {
		t.Fatal(err)
	}
	for _, e := range canon.events {
		if e.Type == "beat_committed" {
			var value struct{ Text string }
			_ = json.Unmarshal(e.Payload, &value)
			if value.Text == "Первый фрагмент черновика" || value.Text == "" {
				t.Fatal("partial text committed")
			}
		}
	}
	last := int64(0)
	ready := false
	for _, u := range sink.updates {
		if u.Provisional {
			if u.Revision <= last {
				t.Fatal("non-monotonic draft revision")
			}
			last = u.Revision
		}
		if u.Phase == PhaseDraftReady || u.Phase == PhaseDraftReplaced {
			ready = true
		}
	}
	if !ready {
		t.Fatal("stream did not finalize when global provisional flag was off")
	}
}
func TestStreamDefaultsOffAndFailureNeverCommits(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		model := &pipelineStreamFake{StoryLLM: scripted(), fail: true}
		canon := &appender{}
		sink := &memSink{}
		_, err := (Pipeline{LLM: model, Canon: canon, Targets: targetSource{}, Sink: sink, StreamWriter: enabled}).Run(context.Background(), mustID(1), scheduleAction())
		if enabled {
			if err == nil || len(canon.events) != 0 || sink.updates[len(sink.updates)-1].Phase != PhaseFailed {
				t.Fatal("failed stream committed")
			}
		} else if err != nil || model.callbacks != 0 {
			t.Fatal("default mode started streaming")
		}
	}
}
func TestStreamSnapshotsStayOutOfSQLAndReconnectUsesOnlyLatest(t *testing.T) {
	ctx := context.Background()
	gid := mustID(1)
	store := &updateMem{}
	hub := NewHub()
	sink := PersistedSink{Store: store, Live: hub}
	sink.Publish(ctx, Update{GenerationID: gid, Phase: PhaseWriting})
	for revision := int64(1); revision <= 40; revision++ {
		sink.Publish(ctx, Update{GenerationID: gid, Phase: PhaseDraftStreaming, Revision: revision, Provisional: true, TextDelta: "prefix"})
	}
	rows, _ := store.List(ctx, gid, 0)
	if len(rows) != 1 {
		t.Fatal("token snapshots persisted")
	}
	runner := &DurableRunner{Updates: store, Hub: hub}
	replay, err := runner.Replay(ctx, gid, 0)
	if err != nil || len(replay) != 2 || replay[1].Revision != 40 || replay[1].Sequence != 0 {
		t.Fatal("reconnect lost latest transient snapshot")
	}
	sink.Publish(ctx, Update{GenerationID: gid, Phase: PhaseDraftReady, Revision: 41, Provisional: true, TextDelta: "final"})
	if _, ok := hub.LatestDraft(gid); ok {
		t.Fatal("finished stream retained stale snapshot")
	}
	rows, _ = store.List(ctx, gid, 0)
	if len(rows) != 2 || rows[1].TextDelta != "final" {
		t.Fatal("final text not persisted")
	}
}
func TestRetryEpochRejectsPreviousAttemptDraftAndClearsHub(t *testing.T) {
	ctx := context.Background()
	gid := mustID(1)
	hub := NewHub()
	sink := attemptAwareSink{Sink: hub, retryRemaining: true, revisionBase: 1000000}
	sink.Publish(ctx, Update{GenerationID: gid, Phase: PhaseDraftStreaming, Provisional: true, Revision: 4, TextDelta: "old"})
	u, _ := hub.LatestDraft(gid)
	if u.Revision != 1000004 {
		t.Fatal("attempt epoch missing")
	}
	ch, cancel := hub.Subscribe(gid, 4)
	defer cancel()
	sink.Publish(ctx, Update{GenerationID: gid, Phase: PhaseFailed})
	retry := <-ch
	if retry.Phase != PhaseRetrying || retry.Revision != 1999999 {
		t.Fatal("retry barrier missing")
	}
	if _, ok := hub.LatestDraft(gid); ok {
		t.Fatal("retry retained stale draft")
	}
}

type rewritingStreamFake struct {
	ai.StoryLLM
	calls int
	old   string
}

func (f *rewritingStreamFake) Generate(ctx context.Context, r ai.StoryRequest) (ai.StoryResponse, error) {
	if r.Role == "writer" {
		f.calls++
		if f.calls == 1 {
			if r.OnText != nil {
				r.OnText(f.old)
			}
			raw, _ := json.Marshal(map[string]string{"text": f.old})
			return ai.StoryResponse{Output: raw}, nil
		}
	}
	response, err := f.StoryLLM.Generate(ctx, r)
	if r.Role == "writer" && r.OnText != nil {
		var draft struct{ Text string }
		_ = json.Unmarshal(response.Output, &draft)
		r.OnText(draft.Text)
	}
	return response, err
}
func TestStreamingSemanticRewriteRetractsRejectedText(t *testing.T) {
	old := "Илья остановился у заросшего окна и внимательно посмотрел на пустую дорогу. Ветер раскачивал сухую ветку, а за ставнями никто не отвечал. Он ещё раз проверил засов и вернулся к спутнице."
	model := &rewritingStreamFake{StoryLLM: scripted(), old: old}
	target, _ := (targetSource{}).CurrentWriteTarget(context.Background(), "")
	target.PreviousBeatText = old
	canon := &appender{}
	sink := &memSink{}
	_, err := (Pipeline{LLM: model, Canon: canon, Targets: staticTarget{target: target}, Sink: sink, StreamWriter: true}).Run(context.Background(), mustID(1), scheduleAction())
	if err != nil || model.calls != 2 {
		t.Fatalf("rewrite not exercised: %v", err)
	}
	showedOld, cleared, replaced := false, false, false
	for _, u := range sink.updates {
		if u.Phase == PhaseDraftStreaming && u.TextDelta == old {
			showedOld = true
		}
		if showedOld && u.Phase == PhaseDraftStreaming && u.TextDelta == "" {
			cleared = true
		}
		if u.Phase == PhaseDraftReplaced && u.TextDelta != "" && u.TextDelta != old {
			replaced = true
		}
	}
	if !showedOld || !cleared || !replaced {
		t.Fatal("rejected streamed prose was not explicitly replaced")
	}
	for _, e := range canon.events {
		if e.Type == "beat_committed" {
			var draft struct{ Text string }
			_ = json.Unmarshal(e.Payload, &draft)
			if draft.Text == old {
				t.Fatal("rejected streamed prose reached Canon")
			}
		}
	}
}
