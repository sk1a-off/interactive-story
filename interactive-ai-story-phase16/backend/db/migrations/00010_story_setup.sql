-- +goose Up
CREATE TABLE IF NOT EXISTS story_tags (
    story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
    tag text NOT NULL,
    category text NOT NULL DEFAULT 'custom',
    PRIMARY KEY(story_id, tag)
);

CREATE TABLE story_setup_components (
    story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
    component_key text NOT NULL CHECK(component_key IN (
      'story_bible','player','world','initial_cast','visual_bible','opening_situation'
    )),
    revision integer NOT NULL DEFAULT 1 CHECK(revision>0),
    source text NOT NULL CHECK(source IN ('manual','ai','mixed')),
    payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
    locked boolean NOT NULL DEFAULT false,
    status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','ready','invalid')),
    generation_id uuid NULL REFERENCES generation_jobs(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(story_id,component_key)
);
CREATE INDEX story_setup_components_story_status_idx ON story_setup_components(story_id,status);

-- Story cannot be started twice.
ALTER TABLE stories ADD COLUMN started_at timestamptz NULL;

-- +goose Down
ALTER TABLE stories DROP COLUMN started_at;
DROP TABLE story_setup_components;
DROP TABLE IF EXISTS story_tags;
