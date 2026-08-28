package bootstrap

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func EnsureLocalUser(ctx context.Context, p *pgxpool.Pool, uid id.ID, username string) error {
	_, e := p.Exec(ctx, `INSERT INTO users(id,username) VALUES($1,$2) ON CONFLICT(id) DO NOTHING`, uid, username)
	return e
}
func EnsureAIConfig(ctx context.Context, p *pgxpool.Pool, story aiport.ProviderIdentity) error {
	var exists bool
	if e := p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_config_revisions)`).Scan(&exists); e != nil {
		return e
	}
	if exists {
		return nil
	}
	var revisionID id.ID
	e := p.QueryRow(ctx, `INSERT INTO ai_config_revisions(story_llm_provider,story_llm_model,story_llm_profile,embedding_provider,embedding_model,image_provider,image_model,settings)
 VALUES($1,$2,$3,'local_sentence_transformers','deepvk/USER2-small','perchance_browser','text-to-image-plugin',
 jsonb_build_object('storyLlmContext',16384,'storyLlmKvDType','q8_0'))
 RETURNING id`, story.Provider, story.Model, story.Profile).Scan(&revisionID)
	if e != nil {
		return e
	}
	_, e = p.Exec(ctx, `INSERT INTO ai_active_config(singleton,config_revision_id) VALUES(true,$1) ON CONFLICT(singleton) DO UPDATE SET config_revision_id=EXCLUDED.config_revision_id,updated_at=now()`, revisionID)
	return e
}
