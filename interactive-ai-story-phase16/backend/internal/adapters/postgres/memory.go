package postgres

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

type MemoryRetriever struct{ q pgxQuerier }
type pgxQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func NewMemoryRetriever(q pgxQuerier) *MemoryRetriever { return &MemoryRetriever{q: q} }

func (r *MemoryRetriever) Search(ctx context.Context, q memory.Query) ([]memory.Memory, error) {
	if len(q.Vector) != 384 {
		return nil, errors.New("expected 384-dimensional embedding")
	}
	if q.Limit < 1 {
		q.Limit = 8
	}
	// Scope and temporal validity are in the SQL WHERE clause, before ORDER BY
	// vector distance. This is an isolation boundary, not merely a relevance hint.
	rows, err := r.q.Query(ctx, `
SELECT id,story_id,timeline_id,owner_type,owner_id,valid_from_event_seq,valid_to_event_seq,kind,content,
       1-(embedding <=> $7::vector) AS score
FROM memory_embeddings
WHERE story_id=$1 AND timeline_id=$2 AND owner_type=$3 AND owner_id=$4
  AND valid_from_event_seq <= $5
  AND (valid_to_event_seq IS NULL OR valid_to_event_seq >= $5)
ORDER BY embedding <=> $7::vector
LIMIT $6`, q.StoryID.String(), id.ID(q.TimelineID).String(), string(q.OwnerType), q.OwnerID.String(), q.AtEventSeq, q.Limit, vectorLiteral(q.Vector))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []memory.Memory
	for rows.Next() {
		var m memory.Memory
		var tid id.ID
		var ot string
		if err := rows.Scan(&m.ID, &m.StoryID, &tid, &ot, &m.OwnerID, &m.ValidFromSeq, &m.ValidToSeq, &m.Kind, &m.Content, &m.Score); err != nil {
			return nil, err
		}
		m.TimelineID = timeline.ID(tid)
		m.OwnerType = memory.OwnerType(ot)
		out = append(out, m)
	}
	return out, rows.Err()
}
func vectorLiteral(v []float32) string {
	b := make([]byte, 0, len(v)*8)
	b = append(b, '[')
	for i, x := range v {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, []byte(floatString(x))...)
	}
	b = append(b, ']')
	return string(b)
}
func floatString(v float32) string { return strconv.FormatFloat(float64(v), 'g', -1, 32) }
