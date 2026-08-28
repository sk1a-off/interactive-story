package postgres

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type GenerationMetadata struct{ pool *pgxpool.Pool }

func NewGenerationMetadata(p *pgxpool.Pool) *GenerationMetadata { return &GenerationMetadata{pool: p} }
func (g *GenerationMetadata) StoryForTimeline(ctx context.Context, tid timeline.ID) (story.ID, error) {
	var sid story.ID
	e := g.pool.QueryRow(ctx, `SELECT story_id FROM timelines WHERE id=$1 AND status<>'deleted'`, tid).Scan(&sid)
	return sid, e
}
