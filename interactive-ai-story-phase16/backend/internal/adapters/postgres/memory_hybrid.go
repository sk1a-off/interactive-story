package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

// HybridMemoryRetriever is opt-in. The legacy retriever and live generation
// wiring are deliberately unchanged until Context Builder validates provenance.
type HybridMemoryRetriever struct{ q pgxQuerier }

func NewHybridMemoryRetriever(q pgxQuerier) *HybridMemoryRetriever {
	return &HybridMemoryRetriever{q: q}
}

func validateHybridQuery(q memory.Query) error {
	if q.StoryID.IsZero() || id.ID(q.TimelineID).IsZero() || q.OwnerID.IsZero() || q.AtEventSeq < 0 {
		return errors.New("invalid memory scope")
	}
	switch q.OwnerType {
	case memory.OwnerTimeline:
		if q.OwnerID != id.ID(q.TimelineID) {
			return errors.New("invalid timeline memory owner")
		}
	case memory.OwnerStory:
		if q.OwnerID != q.StoryID {
			return errors.New("invalid story memory owner")
		}
	case memory.OwnerCharacter:
	default:
		return errors.New("invalid memory owner type")
	}
	if !validMemoryVector(q.Vector) {
		return errors.New("expected finite nonzero 384-dimensional query vector")
	}
	if strings.TrimSpace(q.Profile.Provider) == "" || strings.TrimSpace(q.Profile.Model) == "" || q.Profile.Revision < 1 {
		return errors.New("memory embedding identity required")
	}
	if q.Limit > 100 || len(q.Text) > 16384 {
		return errors.New("memory query exceeds search limits")
	}
	return nil
}

func (r *HybridMemoryRetriever) Search(ctx context.Context, q memory.Query) ([]memory.Memory, error) {
	if err := validateHybridQuery(q); err != nil {
		return nil, err
	}
	plan, err := NewMemoryVisibilityResolver(r.q).Resolve(ctx, q.StoryID, q.TimelineID, q.AtEventSeq)
	if err != nil {
		return nil, err
	}
	if q.Limit < 1 {
		q.Limit = 8
	}
	var timelines []string
	var after, at []int64
	for _, segment := range plan.Segments() {
		timelines = append(timelines, id.ID(segment.TimelineID).String())
		after = append(after, segment.AfterEventSeq)
		at = append(at, segment.AtEventSeq)
	}
	// One statement snapshot covers embeddings and invalidation/publication
	// cursors. Lineage edges resolved above are immutable. MATERIALIZED makes
	// vector ranking exact over eligible rows, without filtered global HNSW.
	rows, err := r.q.Query(ctx, hybridMemorySQL,
		q.StoryID.String(), id.ID(q.TimelineID).String(), string(q.OwnerType), q.OwnerID.String(), q.AtEventSeq,
		q.Profile.Provider, q.Profile.Model, q.Profile.Revision,
		timelines, after, at, vectorLiteral(q.Vector), q.Text, 4*q.Limit, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []memory.Memory
	for rows.Next() {
		var m memory.Memory
		var tid id.ID
		var owner string
		if err = rows.Scan(&m.ID, &m.StoryID, &tid, &owner, &m.OwnerID, &m.ValidFromSeq, &m.ValidToSeq,
			&m.Kind, &m.Content, &m.DocumentClass, &m.SourceKey, &m.Score,
			&m.Profile.Provider, &m.Profile.Model, &m.Profile.Revision); err != nil {
			return nil, err
		}
		m.TimelineID, m.OwnerType = timeline.ID(tid), memory.OwnerType(owner)
		out = append(out, m)
	}
	return out, rows.Err()
}

const hybridMemorySQL = `
WITH segments AS (
 SELECT * FROM unnest($9::uuid[],$10::bigint[],$11::bigint[]) AS s(timeline_id,after_seq,at_seq)
), eligible AS MATERIALIZED (
 SELECT DISTINCT ON (m.timeline_id,m.owner_type,m.owner_id,m.source_key)
        m.*
 FROM memory_embeddings m JOIN segments s ON s.timeline_id=m.timeline_id
 WHERE m.story_id=$1::uuid
   AND m.embedding_provider=$6 AND m.embedding_model=$7 AND m.embedding_revision=$8
   AND m.source_key IS NOT NULL
   AND m.owner_type=$3
   AND m.owner_id=CASE WHEN $3='timeline' THEN s.timeline_id ELSE $4::uuid END
   AND (
     (m.document_class='historical_beat'
      AND m.valid_from_event_seq>s.after_seq AND m.valid_from_event_seq<=s.at_seq
      AND (m.valid_to_event_seq IS NULL OR m.valid_to_event_seq>=s.at_seq))
     OR
     (m.document_class='branch_state' AND m.timeline_id=$2::uuid
      AND m.valid_from_event_seq>=s.after_seq AND m.valid_from_event_seq<=$5
      AND (m.valid_to_event_seq IS NULL OR m.valid_to_event_seq>=$5)
      AND EXISTS (
        SELECT 1 FROM memory_index_state observed
        JOIN memory_index_publication_state published ON published.timeline_id=observed.timeline_id
        WHERE observed.timeline_id=m.timeline_id AND observed.story_id=$1::uuid
          AND published.provider_name=$6 AND published.model_name=$7 AND published.embedding_revision=$8
          AND published.published_event_seq>=observed.observed_event_seq
          AND published.published_event_seq>=$5))
   )
 ORDER BY m.timeline_id,m.owner_type,m.owner_id,m.source_key,m.valid_from_event_seq DESC,m.id
), vector_top AS (
 SELECT id,embedding <=> $12::vector AS distance FROM eligible
 ORDER BY distance,id LIMIT $14
), vector_rank AS (
 SELECT id,row_number() OVER (ORDER BY distance,id) AS rank FROM vector_top
), lexical_top AS (
 SELECT id,ts_rank_cd(search_text,plainto_tsquery('russian'::regconfig,$13)) AS relevance
 FROM eligible WHERE search_text @@ plainto_tsquery('russian'::regconfig,$13)
 ORDER BY relevance DESC,id LIMIT $14
), lexical_rank AS (
 SELECT id,row_number() OVER (ORDER BY relevance DESC,id) AS rank FROM lexical_top
), fused AS (
 SELECT COALESCE(v.id,l.id) AS id,
        COALESCE(1.0/(60+v.rank),0)+COALESCE(1.0/(60+l.rank),0) AS score
 FROM vector_rank v FULL JOIN lexical_rank l ON l.id=v.id
)
SELECT e.id,e.story_id,e.timeline_id,e.owner_type,e.owner_id,e.valid_from_event_seq,e.valid_to_event_seq,
       e.kind,e.content,e.document_class,e.source_key,f.score::double precision,
       e.embedding_provider,e.embedding_model,e.embedding_revision
FROM fused f JOIN eligible e ON e.id=f.id
ORDER BY f.score DESC,e.id LIMIT $15`
