-- +goose Up
-- Keep fresh-schema defaults aligned with the application profile. Existing
-- generations remain immutable and retain their original prompt provenance.
ALTER TABLE image_generations
  ALTER COLUMN style_prompt SET DEFAULT 'cinematic digital illustration, detailed painterly rendering, controlled bold shading, physically coherent global illumination, expressive faces, readable body language, natural anatomy, realistic hands, integrated atmospheric background, strong composition, clear depth, sharp focal subject, rich but balanced color',
  ALTER COLUMN style_negative_prompt SET DEFAULT 'text, letters, words, captions, subtitles, speech bubbles, dialogue balloons, comic book, comic panels, manga panels, storyboard, split screen, collage, multiple frames, contact sheet, watermark, signature, logo, UI, duplicated characters, duplicate face, extra limbs, extra arms, extra legs, extra fingers, fused fingers, malformed hands, deformed anatomy, cropped head, blurry face, flat lighting, cluttered composition, low detail, low quality, jpeg artifacts';

-- +goose Down
ALTER TABLE image_generations
  ALTER COLUMN style_prompt SET DEFAULT 'breathtaking digital art, trending on artstation, by atey ghailan, by greg rutkowski, by greg tocchini, by james gilleard, 8k, high resolution, best quality',
  ALTER COLUMN style_negative_prompt SET DEFAULT 'low-quality, deformed, signature watermark text, poorly drawn';
