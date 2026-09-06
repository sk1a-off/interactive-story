package postgres

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

type GenerationUpdates struct{ pool *pgxpool.Pool }

func NewGenerationUpdates(p *pgxpool.Pool) *GenerationUpdates { return &GenerationUpdates{pool: p} }
func (s *GenerationUpdates) Append(ctx context.Context, u app.Update) (app.Update, error) {
	e := s.pool.QueryRow(ctx, `
 WITH next AS (SELECT COALESCE(MAX(sequence)+1,1) AS seq FROM generation_updates WHERE generation_job_id=$1)
	INSERT INTO generation_updates(generation_job_id,sequence,phase,text_delta,error_text,timeline_id,revision,provisional)
	SELECT $1,seq,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7 FROM next
	RETURNING sequence`, u.GenerationID, string(u.Phase), u.TextDelta, u.Error, u.TimelineID, u.Revision, u.Provisional).Scan(&u.Sequence)
	return u, e
}
func (s *GenerationUpdates) List(ctx context.Context, g id.ID, after int64) ([]app.Update, error) {
	rows, e := s.pool.Query(ctx, `SELECT sequence,phase,text_delta,error_text,COALESCE(timeline_id::text,''),revision,provisional FROM generation_updates WHERE generation_job_id=$1 AND sequence>$2 ORDER BY sequence`, g, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []app.Update
	for rows.Next() {
		var u app.Update
		u.GenerationID = g
		if e = rows.Scan(&u.Sequence, &u.Phase, &u.TextDelta, &u.Error, &u.TimelineID, &u.Revision, &u.Provisional); e != nil {
			return nil, e
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
