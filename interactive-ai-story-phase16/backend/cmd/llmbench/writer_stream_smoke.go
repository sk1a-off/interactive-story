package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

type liveWriterOnly struct{ real, scripted ai.StoryLLM }

func (l liveWriterOnly) Identity() ai.ProviderIdentity { return l.real.Identity() }
func (l liveWriterOnly) Generate(ctx context.Context, r ai.StoryRequest) (ai.StoryResponse, error) {
	if r.Role == "writer" {
		return l.real.Generate(ctx, r)
	}
	return l.scripted.Generate(ctx, r)
}

type streamSmokeSink struct {
	started      time.Time
	first        *float64
	ready        *float64
	snapshots    int
	lastRevision int64
	text         string
}

func (s *streamSmokeSink) Publish(_ context.Context, u appgen.Update) {
	if u.Phase == appgen.PhaseDraftStreaming && u.TextDelta != "" {
		s.snapshots++
	}
	if (u.Phase == appgen.PhaseDraftStreaming || u.Phase == appgen.PhaseDraftReady || u.Phase == appgen.PhaseDraftReplaced) && u.TextDelta != "" {
		if s.first == nil {
			v := float64(time.Since(s.started)) / float64(time.Millisecond)
			s.first = &v
		}
		s.lastRevision = u.Revision
		s.text = u.TextDelta
	}
	if u.Phase == appgen.PhaseDraftReady || u.Phase == appgen.PhaseDraftReplaced {
		v := float64(time.Since(s.started)) / float64(time.Millisecond)
		s.ready = &v
	}
}

type writerStreamSmokeRow struct {
	Stream        bool           `json:"stream"`
	Repeat        int            `json:"repeat"`
	Passed        bool           `json:"passed"`
	FirstTextMS   *float64       `json:"firstTextMs,omitempty"`
	WriterReadyMS *float64       `json:"writerReadyMs,omitempty"`
	TotalMS       float64        `json:"totalMs"`
	Snapshots     int            `json:"snapshots"`
	Choices       int            `json:"choices"`
	Output        string         `json:"output"`
	Calls         []turnRoleCall `json:"calls"`
	Failure       string         `json:"failure,omitempty"`
}

func runWriterStreamSmoke(o turnBenchmarkOptions, output string) (runErr error) {
	if output == "" || o.Repeats < 1 || o.Repeats > 4 {
		return errors.New("writer smoke requires output and 1..4 repeats")
	}
	root, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(root, 25*time.Minute)
	defer cancel()
	var rev aiconfig.Revision
	var prompt promptset.Revision
	var keys []string
	var err error
	if o.Provider == "configured" {
		rev, prompt, keys, err = turnBenchmarkConfigured(ctx, o)
		if err != nil {
			return err
		}
	} else if o.Provider != "fake" {
		return errors.New("configured or fake provider required")
	}
	type report struct {
		Provider     string                 `json:"provider"`
		Status       string                 `json:"status"`
		ConfigID     id.ID                  `json:"configId"`
		PromptSHA256 string                 `json:"promptSha256"`
		Runs         []writerStreamSmokeRow `json:"runs"`
		Notes        []string               `json:"notes"`
	}
	raw, _ := json.Marshal(prompt)
	hash := sha256.Sum256(raw)
	result := report{Provider: o.Provider, Status: "running", ConfigID: rev.ID, PromptSHA256: hex.EncodeToString(hash[:]), Notes: []string{"Production Pipeline with live configured writer and scripted other roles. In-memory Canon capture, no SQL/story/index writes and no browser timing.", "Both arms share frozen input and pinned provider/prompt revision. Generated prose can differ; this is a compatibility/availability smoke, not a literary or full-turn speed claim."}}
	if o.Provider == "fake" {
		result.Notes[0] = "All providers are scripted; no HTTP, SQL, or browser calls. Mechanical Pipeline/Canon capture only."
	}
	save := func() error {
		body, e := json.MarshalIndent(result, "", "  ")
		if e != nil {
			return e
		}
		return os.WriteFile(output, append(body, '\n'), 0600)
	}
	defer func() {
		if err := save(); err != nil {
			runErr = err
		}
	}()
	f := turnFixtures()[0]
	for repeat := 0; repeat < o.Repeats; repeat++ {
		arms := []bool{false, true}
		if repeat%2 == 1 {
			arms = []bool{true, false}
		}
		for _, stream := range arms {
			model, err := turnBenchmarkModel(o, rev, prompt, keys, f)
			if err != nil {
				return err
			}
			if o.Provider == "fake" && stream {
				model = smokeStreamingFake{StoryLLM: model}
			}
			started := time.Now()
			observer := &turnObserver{base: liveWriterOnly{real: model, scripted: fakeTurnModel(f)}, started: started}
			sink := &streamSmokeSink{started: started}
			canon := &turnCaptureCanon{}
			pipeline := appgen.Pipeline{LLM: observer, Planner: appgen.LLMTurnPlanner{LLM: observer}, Canon: canon, Targets: preparedTurnFixture{Fixture: f}, Sink: sink, StreamWriter: stream, ProvisionalBeat: true, MaxRepairs: 1}
			gid, _ := id.New()
			tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000008010"))
			_, err = pipeline.Run(ctx, gid, appgen.PlayerAction{TimelineID: tid, Text: f.Action})
			row := writerStreamSmokeRow{Stream: stream, Repeat: repeat + 1, Passed: err == nil && canon.choices == 4, FirstTextMS: sink.first, WriterReadyMS: sink.ready, TotalMS: float64(time.Since(started)) / float64(time.Millisecond), Snapshots: sink.snapshots, Choices: canon.choices, Output: sink.text, Calls: observer.calls}
			if err != nil {
				row.Failure = "pipeline_failed"
			}
			result.Runs = append(result.Runs, row)
			if e := save(); e != nil {
				return e
			}
			fmt.Printf("writer stream=%t passed=%t snapshots=%d total_ms=%.0f\n", stream, row.Passed, row.Snapshots, row.TotalMS)
		}
	}
	result.Status = "measured"
	for _, r := range result.Runs {
		if !r.Passed {
			result.Status = "failed"
		}
	}
	if result.Status == "failed" {
		return errors.New("writer streaming smoke failed; inspect saved report")
	}
	if o.Provider == "fake" && result.Status == "measured" {
		result.Status = "smoke_only"
	}
	return nil
}

type smokeStreamingFake struct{ ai.StoryLLM }

func (s smokeStreamingFake) Generate(ctx context.Context, r ai.StoryRequest) (ai.StoryResponse, error) {
	response, err := s.StoryLLM.Generate(ctx, r)
	if r.OnText != nil {
		var text struct{ Text string }
		_ = json.Unmarshal(response.Output, &text)
		for _, part := range []string{"Начало черновика", text.Text} {
			r.OnText(part)
		}
	}
	return response, err
}
