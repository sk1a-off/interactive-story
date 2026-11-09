package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/memory"
	"github.com/local/interactive-ai-story/backend/internal/domain/projection"
	"github.com/local/interactive-ai-story/backend/internal/domain/snapshot"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/memoryindexrepo"
)

type memoryIndexDB interface {
	memoryQueueDB
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type MemoryIndexStore struct{ db memoryIndexDB }

func NewMemoryIndexStore(db memoryIndexDB) *MemoryIndexStore { return &MemoryIndexStore{db: db} }

func (s *MemoryIndexStore) Load(ctx context.Context, job repo.Job) ([]domain.Document, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var kind string
	var sourceID id.ID
	var at int64
	if err = tx.QueryRow(ctx, `SELECT source_kind,COALESCE(source_event_id,source_snapshot_id)::text,source_event_seq FROM memory_index_jobs WHERE id=$1 AND story_id=$2 AND timeline_id=$3`, job.ID, job.StoryID, job.TimelineID).Scan(&kind, &sourceID, &at); err != nil {
		return nil, err
	}
	if kind != job.SourceKind || sourceID != job.SourceID || at != job.SourceEventSeq {
		return nil, errors.New("memory source job mismatch")
	}
	plan, err := NewMemoryVisibilityResolver(tx).Resolve(ctx, job.StoryID, job.TimelineID, at)
	if err != nil {
		return nil, err
	}
	segments := plan.Segments()
	state := projection.Empty(job.TimelineID)
	var snapshotID id.ID
	if kind == "snapshot" {
		snapshotID = sourceID
	} else {
		err = tx.QueryRow(ctx, `SELECT id::text FROM snapshots WHERE timeline_id=$1 AND event_seq<=$2 ORDER BY event_seq DESC LIMIT 1`, job.TimelineID, at).Scan(&snapshotID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	if !snapshotID.IsZero() {
		base, err := (snapshotRepository{tx: tx}).Get(ctx, snapshot.ID(snapshotID))
		if err != nil {
			return nil, err
		}
		if base.TimelineID != job.TimelineID || base.EventSeq > at {
			return nil, errors.New("memory snapshot scope mismatch")
		}
		state = base.State
	} else if len(segments) > 1 {
		return nil, errors.New("fork baseline snapshot unavailable")
	}
	rows, err := tx.Query(ctx, `SELECT id::text,seq,event_type,schema_version,payload FROM story_events WHERE timeline_id=$1 AND seq>$2 AND seq<=$3 ORDER BY seq`, job.TimelineID, state.LastEventSeq, at)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		stored := event.StoredEvent{TimelineID: job.TimelineID}
		if err = rows.Scan(&stored.ID, &stored.Seq, &stored.Type, &stored.SchemaVersion, &stored.Payload); err != nil {
			rows.Close()
			return nil, err
		}
		state, err = projection.Apply(state, stored)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("memory replay seq=%d: %w", stored.Seq, err)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if state.LastEventSeq != at {
		return nil, errors.New("memory checkpoint replay is incomplete")
	}
	documents, err := domain.StateDocuments(state)
	if err != nil {
		return nil, err
	}
	if kind == "event" {
		var eventType string
		var payload []byte
		if err = tx.QueryRow(ctx, `SELECT event_type,payload FROM story_events WHERE id=$1 AND timeline_id=$2 AND seq=$3`, sourceID, job.TimelineID, at).Scan(&eventType, &payload); err != nil {
			return nil, err
		}
		if eventType == "beat_committed" {
			var beat struct {
				Text string `json:"text"`
			}
			if err = json.Unmarshal(payload, &beat); err != nil {
				return nil, err
			}
			if strings.TrimSpace(beat.Text) != "" {
				documents = append(documents, domain.Document{Key: "event:" + sourceID.String(), Class: "historical_beat", Kind: "beat", OwnerType: "timeline", OwnerID: id.ID(job.TimelineID), Content: beat.Text, FromSeq: at})
			}
		}
	}
	return documents, tx.Commit(ctx)
}
