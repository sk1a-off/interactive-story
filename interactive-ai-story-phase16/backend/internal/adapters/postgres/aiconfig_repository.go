package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

type AIConfigRepository struct{ pool *pgxpool.Pool }

func NewAIConfigRepository(p *pgxpool.Pool) *AIConfigRepository { return &AIConfigRepository{pool: p} }
func (r *AIConfigRepository) CreateRevision(ctx context.Context, c aiconfig.CreateRevisionCommand) (aiconfig.Revision, error) {
	if e := c.Validate(); e != nil {
		return aiconfig.Revision{}, e
	}
	var v aiconfig.Revision
	var settings []byte
	e := r.pool.QueryRow(ctx, `INSERT INTO ai_config_revisions(story_llm_provider,story_llm_model,story_llm_profile,embedding_provider,embedding_model,image_provider,image_model,settings)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8)
 RETURNING id,revision,story_llm_provider,story_llm_model,story_llm_profile,embedding_provider,embedding_model,image_provider,image_model,settings`,
		c.StoryLLM.Provider, c.StoryLLM.Model, c.StoryLLM.Profile, c.Embedding.Provider, c.Embedding.Model, c.Image.Provider, c.Image.Model, []byte(c.Public.JSON())).
		Scan(&v.ID, &v.Revision, &v.StoryLLM.Provider, &v.StoryLLM.Model, &v.StoryLLM.Profile, &v.Embedding.Provider, &v.Embedding.Model, &v.Image.Provider, &v.Image.Model, &settings)
	if e != nil {
		return v, e
	}
	v.StoryLLM.Kind = ai.KindStoryLLM
	v.Embedding.Kind = ai.KindEmbedding
	v.Image.Kind = ai.KindImage
	v.Settings = json.RawMessage(settings)
	return v, nil
}
func (r *AIConfigRepository) Get(ctx context.Context, i id.ID) (aiconfig.Revision, error) {
	return r.one(ctx, `SELECT id,revision,story_llm_provider,story_llm_model,story_llm_profile,embedding_provider,embedding_model,image_provider,image_model,settings FROM ai_config_revisions WHERE id=$1`, i)
}
func (r *AIConfigRepository) Active(ctx context.Context) (aiconfig.Revision, error) {
	return r.one(ctx, `SELECT c.id,c.revision,c.story_llm_provider,c.story_llm_model,c.story_llm_profile,c.embedding_provider,c.embedding_model,c.image_provider,c.image_model,c.settings FROM ai_config_revisions c JOIN ai_active_config a ON a.config_revision_id=c.id WHERE a.singleton=true`)
}
func (r *AIConfigRepository) one(ctx context.Context, q string, args ...any) (aiconfig.Revision, error) {
	var v aiconfig.Revision
	var settings []byte
	e := r.pool.QueryRow(ctx, q, args...).Scan(&v.ID, &v.Revision, &v.StoryLLM.Provider, &v.StoryLLM.Model, &v.StoryLLM.Profile, &v.Embedding.Provider, &v.Embedding.Model, &v.Image.Provider, &v.Image.Model, &settings)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if e != nil {
		return v, e
	}
	v.StoryLLM.Kind = ai.KindStoryLLM
	v.Embedding.Kind = ai.KindEmbedding
	v.Image.Kind = ai.KindImage
	v.Settings = settings
	return v, nil
}
func (r *AIConfigRepository) List(ctx context.Context) ([]aiconfig.Revision, error) {
	rows, e := r.pool.Query(ctx, `SELECT id,revision,story_llm_provider,story_llm_model,story_llm_profile,embedding_provider,embedding_model,image_provider,image_model,settings FROM ai_config_revisions ORDER BY revision DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []aiconfig.Revision
	for rows.Next() {
		var v aiconfig.Revision
		var settings []byte
		if e = rows.Scan(&v.ID, &v.Revision, &v.StoryLLM.Provider, &v.StoryLLM.Model, &v.StoryLLM.Profile, &v.Embedding.Provider, &v.Embedding.Model, &v.Image.Provider, &v.Image.Model, &settings); e != nil {
			return nil, e
		}
		v.StoryLLM.Kind = ai.KindStoryLLM
		v.Embedding.Kind = ai.KindEmbedding
		v.Image.Kind = ai.KindImage
		v.Settings = settings
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *AIConfigRepository) Activate(ctx context.Context, i id.ID) error {
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var exists bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_config_revisions WHERE id=$1)`, i).Scan(&exists); e != nil {
		return e
	}
	if !exists {
		return ErrNotFound
	}
	_, e = tx.Exec(ctx, `INSERT INTO ai_active_config(singleton,config_revision_id,updated_at) VALUES(true,$1,now()) ON CONFLICT(singleton) DO UPDATE SET config_revision_id=EXCLUDED.config_revision_id,updated_at=now()`, i)
	if e != nil {
		return fmt.Errorf("activate config: %w", e)
	}
	return tx.Commit(ctx)
}
