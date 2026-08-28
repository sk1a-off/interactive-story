-- +goose Up
ALTER TABLE save_points
    ADD COLUMN display_slot integer NULL CHECK (display_slot IS NULL OR display_slot > 0),
    ADD COLUMN thumbnail_status text NOT NULL DEFAULT 'none'
        CHECK (thumbnail_status IN ('none','queued','ready','failed')),
    ADD COLUMN thumbnail_asset_id uuid NULL;

CREATE UNIQUE INDEX save_points_manual_slot_unique
    ON save_points(timeline_id, display_slot)
    WHERE display_slot IS NOT NULL AND kind='manual';

-- Save correctness never depends on a thumbnail: the asset reference is optional
-- and thumbnail lifecycle is operational rather than Canon.
CREATE INDEX save_points_pinned_created_idx
    ON save_points(timeline_id, pinned DESC, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS save_points_pinned_created_idx;
DROP INDEX IF EXISTS save_points_manual_slot_unique;
ALTER TABLE save_points
    DROP COLUMN IF EXISTS thumbnail_asset_id,
    DROP COLUMN IF EXISTS thumbnail_status,
    DROP COLUMN IF EXISTS display_slot;
