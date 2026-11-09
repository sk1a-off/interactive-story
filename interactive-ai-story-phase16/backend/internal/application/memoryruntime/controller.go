package memoryruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/application/contextbuilder"
	"github.com/local/interactive-ai-story/backend/internal/application/memorycontext"
	"github.com/local/interactive-ai-story/backend/internal/application/memoryindex"
	"github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/aiconfigrepo"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
	indexrepo "github.com/local/interactive-ai-story/backend/internal/ports/memoryindexrepo"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/memoryruntimerepo"
)

var ErrDisabled = errors.New("memory is disabled")

type Controller struct {
	Configs         aiconfigrepo.Repository
	State           repo.Repository
	OwnerID         id.ID
	DefaultEndpoint string
	Embeddings      func(aiconfig.Revision, string) (ai.EmbeddingProvider, error)
	Builder         func(ai.EmbeddingProvider, memory.EmbeddingProfile) contextbuilder.Builder
	Worker          func(ai.EmbeddingProvider, indexrepo.Profile) memoryindex.Worker
	Logger          *slog.Logger
}

func (c *Controller) configuration(rev aiconfig.Revision) (aiconfig.PublicSettings, string, string, error) {
	var s aiconfig.PublicSettings
	if err := json.Unmarshal(rev.Settings, &s); err != nil {
		return s, "", "", err
	}
	s = s.WithMemoryDefaults()
	endpoint := strings.TrimRight(strings.TrimSpace(s.EmbeddingEndpoint), "/")
	if endpoint == "" {
		endpoint = strings.TrimRight(c.DefaultEndpoint, "/")
	}
	raw, _ := json.Marshal([]string{c.OwnerID.String(), rev.Embedding.Provider, rev.Embedding.Model, endpoint})
	hash := sha256.Sum256(raw)
	return s, endpoint, hex.EncodeToString(hash[:]), nil
}
func (c *Controller) ForRevision(ctx context.Context, rev aiconfig.Revision) *memorycontext.Service {
	s, endpoint, sig, err := c.configuration(rev)
	if err != nil || !s.MemoryEnabled || c.State == nil || c.Embeddings == nil || c.Builder == nil {
		return nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	p, err := c.State.Serving(lookupCtx, sig)
	if err != nil {
		return nil
	}
	e, err := c.Embeddings(rev, endpoint)
	if err != nil {
		return nil
	}
	profile := memory.EmbeddingProfile{Provider: p.Provider, Model: p.Model, Revision: p.Revision}
	return &memorycontext.Service{Builder: c.Builder(e, profile), TopK: *s.MemoryTopK, MemoryTokenBudget: *s.MemoryTokenBudget, Logger: c.Logger}
}
func (c *Controller) MemoryStatus(ctx context.Context) (repo.Status, error) {
	rev, err := c.Configs.Active(ctx)
	if err != nil {
		return repo.Status{}, err
	}
	s, _, sig, err := c.configuration(rev)
	if err != nil {
		return repo.Status{}, err
	}
	status, err := c.State.Status(ctx, sig)
	status.Enabled = s.MemoryEnabled
	if !s.MemoryEnabled {
		status.State = "disabled"
	}
	return status, err
}
func (c *Controller) MemoryReindex(ctx context.Context) (repo.Status, error) {
	rev, err := c.Configs.Active(ctx)
	if err != nil {
		return repo.Status{}, err
	}
	s, _, sig, err := c.configuration(rev)
	if err != nil {
		return repo.Status{}, err
	}
	if !s.MemoryEnabled {
		return repo.Status{}, ErrDisabled
	}
	if _, err = c.State.Ensure(ctx, rev.ID, sig, rev.Embedding.Provider, rev.Embedding.Model, true); err != nil {
		return repo.Status{}, err
	}
	return c.MemoryStatus(ctx)
}
func (c *Controller) Tick(ctx context.Context) error {
	rev, err := c.Configs.Active(ctx)
	if err != nil {
		return err
	}
	s, endpoint, sig, err := c.configuration(rev)
	if err != nil {
		return err
	}
	if !s.MemoryEnabled {
		return nil
	}
	if _, err = c.State.Ensure(ctx, rev.ID, sig, rev.Embedding.Provider, rev.Embedding.Model, false); err != nil {
		return err
	}
	profiles, err := c.State.Work(ctx, sig)
	if err != nil {
		return err
	}
	e, err := c.Embeddings(rev, endpoint)
	if err != nil {
		return err
	}
	for _, p := range profiles {
		worker := c.Worker(e, indexrepo.Profile{Provider: p.Provider, Model: p.Model, Revision: p.Revision})
		if err = worker.RunOnce(ctx); err != nil && !errors.Is(err, indexrepo.ErrNoJob) && ctx.Err() == nil && c.Logger != nil {
			c.Logger.WarnContext(ctx, "memory indexing attempt deferred", "index_revision", p.Revision)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = c.State.Finalize(ctx, p.Revision); err != nil {
			return err
		}
	}
	return nil
}
func (c *Controller) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		if err := c.Tick(ctx); err != nil && ctx.Err() == nil && c.Logger != nil {
			c.Logger.WarnContext(ctx, "memory indexing cycle deferred")
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return ctx.Err()
}
