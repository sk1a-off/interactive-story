-- +goose Up
ALTER TABLE image_generations
    DROP CONSTRAINT image_generations_width_check,
    DROP CONSTRAINT image_generations_height_check,
    DROP CONSTRAINT image_generations_image_count_check,
    ADD CONSTRAINT image_generations_resolution_check
        CHECK ((width, height) IN ((768, 768), (512, 768), (768, 512))),
    ADD CONSTRAINT image_generations_image_count_check CHECK (image_count BETWEEN 1 AND 4);

ALTER TABLE scene_images
    DROP CONSTRAINT scene_images_variant_check,
    ADD CONSTRAINT scene_images_variant_check CHECK (variant BETWEEN 1 AND 4);

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM image_generations WHERE width <> 768 OR height <> 768 OR image_count <> 2)
       OR EXISTS (SELECT 1 FROM scene_images WHERE variant NOT IN (1, 2)) THEN
        RAISE EXCEPTION 'Cannot roll back Perchance image settings: existing generations require non-square resolution or a variant count other than two. Retain migration 00036 or explicitly archive the incompatible data first.';
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE image_generations
    DROP CONSTRAINT image_generations_resolution_check,
    DROP CONSTRAINT image_generations_image_count_check,
    ADD CONSTRAINT image_generations_width_check CHECK (width = 768),
    ADD CONSTRAINT image_generations_height_check CHECK (height = 768),
    ADD CONSTRAINT image_generations_image_count_check CHECK (image_count = 2);

ALTER TABLE scene_images
    DROP CONSTRAINT scene_images_variant_check,
    ADD CONSTRAINT scene_images_variant_check CHECK (variant IN (1, 2));
