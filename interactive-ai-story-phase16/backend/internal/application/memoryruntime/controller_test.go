package memoryruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	"github.com/local/interactive-ai-story/backend/internal/application/contextbuilder"
	"github.com/local/interactive-ai-story/backend/internal/application/memoryindex"
	"github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/aiconfigrepo"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
	index "github.com/local/interactive-ai-story/backend/internal/ports/memoryindexrepo"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/memoryruntimerepo"
)

type configs struct {
	aiconfigrepo.Repository
	rev aiconfig.Revision
}

func (c *configs) Active(context.Context) (aiconfig.Revision, error) { return c.rev, nil }

type state struct {
	repo.Repository
	ensures, finalizes, servingCalls int
	force                            bool
	p                                repo.Profile
	status                           repo.Status
	err                              error
}

func (s *state) Ensure(_ context.Context, _ id.ID, _ string, _, _ string, force bool) (repo.Profile, error) {
	s.ensures++
	s.force = force
	return s.p, s.err
}
func (s *state) Serving(context.Context, string) (repo.Profile, error) {
	s.servingCalls++
	return s.p, s.err
}
func (s *state) Work(context.Context, string) ([]repo.Profile, error) {
	return []repo.Profile{s.p}, s.err
}
func (s *state) Finalize(context.Context, int64) error               { s.finalizes++; return nil }
func (s *state) Status(context.Context, string) (repo.Status, error) { return s.status, s.err }

type emptyQueue struct {
	index.Queue
	claims *int
}

func (q emptyQueue) Claim(context.Context, string, time.Time, time.Duration) (index.Job, error) {
	*q.claims++
	return index.Job{}, index.ErrNoJob
}

func TestRuntimeDisabledDoesNoModelOrIndexWorkAndEnabledUsesOnlyServing(t *testing.T) {
	e := fakeai.NewEmbeddingProvider(384)
	cfg := &configs{rev: aiconfig.Revision{ID: "config", Embedding: e.ID, Settings: []byte(`{}`)}}
	s := &state{p: repo.Profile{Revision: 1001, Provider: e.ID.Provider, Model: e.ID.Model}, status: repo.Status{State: "serving", ServingRevision: 1001}}
	claims := 0
	models := 0
	c := &Controller{Configs: cfg, State: s, OwnerID: "owner", DefaultEndpoint: "http://localhost:8090",
		Embeddings: func(aiconfig.Revision, string) (ai.EmbeddingProvider, error) { models++; return e, nil },
		Builder: func(e ai.EmbeddingProvider, p memory.EmbeddingProfile) contextbuilder.Builder {
			return contextbuilder.Builder{Embeddings: e, Profile: p}
		},
		Worker: func(e ai.EmbeddingProvider, p index.Profile) memoryindex.Worker {
			return memoryindex.Worker{Queue: emptyQueue{claims: &claims}, Store: struct{ index.Store }{}, Embeddings: e, Profile: p, Owner: "test"}
		},
	}
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.ForRevision(context.Background(), cfg.rev) != nil || s.ensures != 0 || s.servingCalls != 0 || models != 0 {
		t.Fatal("disabled memory performed work")
	}
	if _, err := c.MemoryReindex(context.Background()); !errors.Is(err, ErrDisabled) {
		t.Fatal("disabled reindex allowed")
	}
	cfg.rev.Settings = []byte(`{"memoryEnabled":true,"memoryTopK":3,"memoryTokenBudget":2048}`)
	if err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.ensures != 1 || s.force || claims != 1 || s.finalizes != 1 || e.Calls != 0 {
		t.Fatal("index work not supervised")
	}
	service := c.ForRevision(context.Background(), cfg.rev)
	if service == nil || service.TopK != 3 || service.MemoryTokenBudget != 2048 || service.Builder.Profile.Revision != 1001 {
		t.Fatal("serving/settings mismatch")
	}
	if _, err := c.MemoryReindex(context.Background()); err != nil || !s.force {
		t.Fatal("explicit rebuild not requested")
	}
	s.err = errors.New("index unavailable")
	if c.ForRevision(context.Background(), cfg.rev) != nil {
		t.Fatal("unavailable index failed open")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("runtime ignored shutdown")
	}
}
func TestSignatureTracksEmbeddingEndpointModelAndOwnerButNotStorySettings(t *testing.T) {
	c := &Controller{OwnerID: "one", DefaultEndpoint: "http://localhost:8090"}
	rev := aiconfig.Revision{Embedding: ai.ProviderIdentity{Provider: "local_sentence_transformers", Model: "384"}, Settings: []byte(`{}`)}
	_, _, first, _ := c.configuration(rev)
	rev.Settings = []byte(`{"memoryEnabled":true,"memoryTopK":16,"storyLlmEndpoint":"http://different"}`)
	_, _, same, _ := c.configuration(rev)
	if same != first {
		t.Fatal("unrelated settings rebuilt embeddings")
	}
	for _, change := range []func(){func() { rev.Embedding.Model = "new" }, func() { rev.Settings = []byte(`{"embeddingEndpoint":"http://other"}`) }, func() { c.OwnerID = "other" }} {
		change()
		_, _, next, _ := c.configuration(rev)
		if next == first {
			t.Fatal("embedding identity reused")
		}
		first = next
	}
}
