package memorycontext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	"github.com/local/interactive-ai-story/backend/internal/application/contextbuilder"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/memory"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

type visibility struct{ plan domain.Visibility }

func (v visibility) Resolve(context.Context, id.ID, timeline.ID, int64) (domain.Visibility, error) {
	return v.plan, nil
}

type store struct {
	rows  []memory.Memory
	query memory.Query
	err   error
}

func (s *store) Search(_ context.Context, q memory.Query) ([]memory.Memory, error) {
	s.query = q
	return s.rows, s.err
}

func TestSupplementBudgetPolicyAndSanitizedFailure(t *testing.T) {
	story, _ := id.New()
	tid, _ := id.New()
	rowID, _ := id.New()
	plan, err := domain.ResolveVisibility(story, timeline.ID(tid), 2, []domain.LineageNode{{StoryID: story, TimelineID: timeline.ID(tid), HeadEventSeq: 2}})
	if err != nil {
		t.Fatal(err)
	}
	e := fakeai.NewEmbeddingProvider(384)
	profile := memory.EmbeddingProfile{Provider: e.ID.Provider, Model: e.ID.Model, Revision: 1}
	data := &store{rows: []memory.Memory{{ID: rowID, StoryID: story, TimelineID: timeline.ID(tid), OwnerType: memory.OwnerTimeline, OwnerID: tid, Profile: profile, Kind: "beat", DocumentClass: "historical_beat", SourceKey: "event:one", Content: "evidence", ValidFromSeq: 1}}}
	var log bytes.Buffer
	s := &Service{Builder: contextbuilder.Builder{Embeddings: e, Memory: data, Visibility: visibility{plan}, Profile: profile}, TopK: 8, MemoryTokenBudget: 1000, Logger: slog.New(slog.NewTextHandler(&log, nil))}
	r := Request{StoryID: story, TimelineID: timeline.ID(tid), AtEventSeq: 2, QueryText: "query", Authoritative: []byte(strings.Repeat("a", 20000))}
	out := s.Build(context.Background(), r)
	if out == nil || out.Policy != EvidencePolicy || len(out.Evidence) != 1 || len(data.query.Kinds) != 4 {
		t.Fatalf("missing bounded supplement: %+v", out)
	}
	raw, _ := json.Marshal(out)
	if (len([]rune(string(raw)))+3)/4 > s.MemoryTokenBudget {
		t.Fatal("supplement exceeded budget with metadata/policy")
	}
	s.MemoryTokenBudget = 1
	before := e.Calls
	if s.Build(context.Background(), r) != nil || e.Calls != before {
		t.Fatal("tiny budget triggered model work")
	}
	s.MemoryTokenBudget = 1000
	data.err = errors.New("SECRET_PROVIDER_ERROR")
	if s.Build(context.Background(), r) != nil || !strings.Contains(log.String(), "memory context skipped") || strings.Contains(log.String(), "SECRET_PROVIDER_ERROR") {
		t.Fatal("unsafe failure/logging")
	}
	s.Builder.Visibility = nil
	before = e.Calls
	if s.Build(context.Background(), r) != nil || e.Calls != before {
		t.Fatal("legacy builder accepted")
	}
}
