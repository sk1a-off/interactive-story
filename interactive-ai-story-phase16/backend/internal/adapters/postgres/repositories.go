package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/projection"
	"github.com/local/interactive-ai-story/backend/internal/domain/savepoint"
	"github.com/local/interactive-ai-story/backend/internal/domain/snapshot"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/repositories"
)

var ErrNotFound = errors.New("postgres: not found")

type canonRepositories struct{ tx pgx.Tx }

func newRepositories(tx pgx.Tx) repositories.CanonRepositories    { return canonRepositories{tx: tx} }
func (r canonRepositories) Stories() repositories.StoryRepository { return storyRepository{tx: r.tx} }
func (r canonRepositories) Timelines() repositories.TimelineRepository {
	return timelineRepository{tx: r.tx}
}
func (r canonRepositories) Events() repositories.EventRepository { return eventRepository{tx: r.tx} }
func (r canonRepositories) Snapshots() repositories.SnapshotRepository {
	return snapshotRepository{tx: r.tx}
}
func (r canonRepositories) SavePoints() repositories.SavePointRepository {
	return savePointRepository{tx: r.tx}
}
func (r canonRepositories) Narrative() repositories.NarrativeProjectionRepository {
	return narrativeProjectionRepository{tx: r.tx}
}

type storyRepository struct{ tx pgx.Tx }

func (r storyRepository) Create(ctx context.Context, value story.Story) error {
	if err := value.Validate(); err != nil {
		return err
	}
	_, err := r.tx.Exec(ctx, `
        INSERT INTO stories (id, owner_id, title, description, status, semantic_revision, created_at, updated_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		value.ID, value.OwnerID, value.Title, value.Description, value.Status, value.SemanticRevision, value.CreatedAt, value.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert story: %w", err)
	}
	return nil
}

func (r storyRepository) Get(ctx context.Context, storyID story.ID) (story.Story, error) {
	var value story.Story
	err := r.tx.QueryRow(ctx, `
        SELECT id, owner_id, title, description, status, semantic_revision, created_at, updated_at
        FROM stories WHERE id=$1`, storyID).Scan(
		&value.ID, &value.OwnerID, &value.Title, &value.Description, &value.Status, &value.SemanticRevision, &value.CreatedAt, &value.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return story.Story{}, ErrNotFound
	}
	if err != nil {
		return story.Story{}, fmt.Errorf("get story: %w", err)
	}
	return value, value.Validate()
}

type timelineRepository struct{ tx pgx.Tx }

func (r timelineRepository) Create(ctx context.Context, value timeline.Timeline) error {
	_, err := r.tx.Exec(ctx, `
        INSERT INTO timelines (id, story_id, parent_timeline_id, forked_from_save_id, name, status, head_event_seq, head_snapshot_id)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		value.ID, value.StoryID, value.ParentTimelineID, value.ForkedFromSaveID, value.Name, value.Status, value.HeadEventSeq, value.HeadSnapshotID)
	if err != nil {
		return fmt.Errorf("insert timeline: %w", err)
	}
	return nil
}

func (r timelineRepository) Get(ctx context.Context, timelineID timeline.ID) (timeline.Timeline, error) {
	return r.get(ctx, timelineID, false)
}

func (r timelineRepository) Lock(ctx context.Context, timelineID timeline.ID) (timeline.Timeline, error) {
	return r.get(ctx, timelineID, true)
}

func (r timelineRepository) get(ctx context.Context, timelineID timeline.ID, lock bool) (timeline.Timeline, error) {
	query := `SELECT id, story_id, parent_timeline_id, forked_from_save_id, name, status, head_event_seq, head_snapshot_id FROM timelines WHERE id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	var value timeline.Timeline
	err := r.tx.QueryRow(ctx, query, timelineID).Scan(&value.ID, &value.StoryID, &value.ParentTimelineID, &value.ForkedFromSaveID, &value.Name, &value.Status, &value.HeadEventSeq, &value.HeadSnapshotID)
	if errors.Is(err, pgx.ErrNoRows) {
		return timeline.Timeline{}, ErrNotFound
	}
	if err != nil {
		return timeline.Timeline{}, fmt.Errorf("get timeline: %w", err)
	}
	return value, nil
}

func (r timelineRepository) AdvanceHead(ctx context.Context, timelineID timeline.ID, newSeq int64) error {
	tag, err := r.tx.Exec(ctx, `UPDATE timelines SET head_event_seq=$2, semantic_revision=semantic_revision+1, updated_at=now() WHERE id=$1`, timelineID, newSeq)
	if err != nil {
		return fmt.Errorf("advance timeline head: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (r timelineRepository) SetHeadSnapshot(ctx context.Context, timelineID timeline.ID, snapshotID snapshot.ID) error {
	tag, err := r.tx.Exec(ctx, `UPDATE timelines SET head_snapshot_id=$2, updated_at=now() WHERE id=$1`, timelineID, snapshotID)
	if err != nil {
		return fmt.Errorf("set head snapshot: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

type eventRepository struct{ tx pgx.Tx }

func (r eventRepository) Append(ctx context.Context, timelineID timeline.ID, values []event.StoredEvent) error {
	if len(values) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, value := range values {
		if value.TimelineID != timelineID {
			return event.ErrInvalidEvent
		}
		if err := value.Validate(); err != nil {
			return err
		}
		batch.Queue(`INSERT INTO story_events (id,timeline_id,seq,event_type,schema_version,payload,generation_id,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, value.ID, value.TimelineID, value.Seq, value.Type, value.SchemaVersion, []byte(value.Payload), value.GenerationID, value.CreatedAt)
	}
	results := r.tx.SendBatch(ctx, batch)
	defer results.Close()
	for range values {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("append story event: %w", err)
		}
	}
	return nil
}

func (r eventRepository) List(ctx context.Context, timelineID timeline.ID, afterSeq int64) ([]event.StoredEvent, error) {
	rows, err := r.tx.Query(ctx, `
        SELECT id,timeline_id,seq,event_type,schema_version,payload,generation_id,created_at
        FROM story_events WHERE timeline_id=$1 AND seq>$2 ORDER BY seq`, timelineID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("list story events: %w", err)
	}
	defer rows.Close()
	var out []event.StoredEvent
	for rows.Next() {
		var value event.StoredEvent
		var payload []byte
		if err := rows.Scan(&value.ID, &value.TimelineID, &value.Seq, &value.Type, &value.SchemaVersion, &payload, &value.GenerationID, &value.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan story event: %w", err)
		}
		value.Payload = json.RawMessage(payload)
		if err := value.Validate(); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate story events: %w", err)
	}
	return out, nil
}

type snapshotRepository struct{ tx pgx.Tx }

func (r snapshotRepository) Create(ctx context.Context, value snapshot.Snapshot) error {
	if err := value.Verify(); err != nil {
		return err
	}
	raw, err := projection.CanonicalJSON(value.State)
	if err != nil {
		return err
	}
	_, err = r.tx.Exec(ctx, `
        INSERT INTO snapshots (id,timeline_id,event_seq,state,state_hash,schema_version,serializer_version,created_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, value.ID, value.TimelineID, value.EventSeq, raw, value.StateHash, value.SchemaVersion, value.SerializerVersion, value.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}
	return nil
}

func (r snapshotRepository) Get(ctx context.Context, snapshotID snapshot.ID) (snapshot.Snapshot, error) {
	return r.get(ctx, `WHERE id=$1`, snapshotID)
}

func (r snapshotRepository) Latest(ctx context.Context, timelineID timeline.ID) (snapshot.Snapshot, error) {
	return r.get(ctx, `WHERE timeline_id=$1 ORDER BY event_seq DESC LIMIT 1`, timelineID)
}

func (r snapshotRepository) get(ctx context.Context, clause string, arg any) (snapshot.Snapshot, error) {
	var value snapshot.Snapshot
	var raw []byte
	query := `SELECT id,timeline_id,event_seq,state,state_hash,schema_version,serializer_version,created_at FROM snapshots ` + clause
	err := r.tx.QueryRow(ctx, query, arg).Scan(&value.ID, &value.TimelineID, &value.EventSeq, &raw, &value.StateHash, &value.SchemaVersion, &value.SerializerVersion, &value.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return snapshot.Snapshot{}, ErrNotFound
	}
	if err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("get snapshot: %w", err)
	}
	if err := json.Unmarshal(raw, &value.State); err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("decode snapshot: %w", err)
	}
	if err := value.Verify(); err != nil {
		return snapshot.Snapshot{}, err
	}
	return value, nil
}

func (r timelineRepository) ListByStory(ctx context.Context, storyID story.ID) ([]timeline.Timeline, error) {
	rows, err := r.tx.Query(ctx, `
        SELECT id, story_id, parent_timeline_id, forked_from_save_id, name, status, head_event_seq, head_snapshot_id
        FROM timelines WHERE story_id=$1 AND status <> 'deleted' ORDER BY created_at, id`, storyID)
	if err != nil {
		return nil, fmt.Errorf("list timelines: %w", err)
	}
	defer rows.Close()
	var out []timeline.Timeline
	for rows.Next() {
		var value timeline.Timeline
		if err := rows.Scan(&value.ID, &value.StoryID, &value.ParentTimelineID, &value.ForkedFromSaveID, &value.Name, &value.Status, &value.HeadEventSeq, &value.HeadSnapshotID); err != nil {
			return nil, fmt.Errorf("scan timeline: %w", err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate timelines: %w", err)
	}
	return out, nil
}

type savePointRepository struct{ tx pgx.Tx }

func (r savePointRepository) Create(ctx context.Context, value savepoint.SavePoint) error {
	if err := value.Validate(); err != nil {
		return err
	}
	_, err := r.tx.Exec(ctx, `
        INSERT INTO save_points(id,story_id,timeline_id,snapshot_id,event_seq,name,note,kind,pinned,chapter_id,scene_id,beat_id,created_at)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		value.ID, value.StoryID, value.TimelineID, value.SnapshotID, value.EventSeq, value.Name, value.Note, value.Kind, value.Pinned, value.ChapterID, value.SceneID, value.BeatID, value.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert save point: %w", err)
	}
	return nil
}

func (r savePointRepository) Get(ctx context.Context, saveID savepoint.ID) (savepoint.SavePoint, error) {
	var value savepoint.SavePoint
	err := r.tx.QueryRow(ctx, `
        SELECT id,story_id,timeline_id,snapshot_id,event_seq,name,note,kind,pinned,chapter_id,scene_id,beat_id,created_at
        FROM save_points WHERE id=$1`, saveID).Scan(
		&value.ID, &value.StoryID, &value.TimelineID, &value.SnapshotID, &value.EventSeq, &value.Name, &value.Note, &value.Kind, &value.Pinned, &value.ChapterID, &value.SceneID, &value.BeatID, &value.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return savepoint.SavePoint{}, ErrNotFound
	}
	if err != nil {
		return savepoint.SavePoint{}, fmt.Errorf("get save point: %w", err)
	}
	return value, value.Validate()
}

func (r savePointRepository) UpdatePresentation(ctx context.Context, saveID savepoint.ID, slot *int, pinned bool) error {
	_, err := r.tx.Exec(ctx, `UPDATE save_points SET display_slot=$2,pinned=$3 WHERE id=$1`, saveID, slot, pinned)
	if err != nil {
		return fmt.Errorf("update save presentation: %w", err)
	}
	return nil
}

func (r savePointRepository) ListByTimeline(ctx context.Context, timelineID timeline.ID) ([]savepoint.SavePoint, error) {
	rows, err := r.tx.Query(ctx, `
        SELECT id,story_id,timeline_id,snapshot_id,event_seq,name,note,kind,pinned,chapter_id,scene_id,beat_id,created_at,display_slot,thumbnail_status,thumbnail_asset_id
        FROM save_points WHERE timeline_id=$1 ORDER BY created_at DESC,id`, timelineID)
	if err != nil {
		return nil, fmt.Errorf("list save points: %w", err)
	}
	defer rows.Close()
	var out []savepoint.SavePoint
	for rows.Next() {
		var value savepoint.SavePoint
		if err := rows.Scan(&value.ID, &value.StoryID, &value.TimelineID, &value.SnapshotID, &value.EventSeq, &value.Name, &value.Note, &value.Kind, &value.Pinned, &value.ChapterID, &value.SceneID, &value.BeatID, &value.CreatedAt, &value.DisplaySlot, &value.ThumbnailStatus, &value.ThumbnailAssetID); err != nil {
			return nil, fmt.Errorf("scan save point: %w", err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate save points: %w", err)
	}
	return out, nil
}
