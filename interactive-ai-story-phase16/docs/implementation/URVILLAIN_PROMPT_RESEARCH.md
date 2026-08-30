# URVILLAIN Imagine prompt research

Source inspected on 2026-08-23: <https://perchance.org/urvillain-imagine>

## Observed generation pipeline

The page is a custom Perchance generator, not a thin call to the standard plugin. Its current source imports a custom `urv-image-plugin` wrapper for Flux Schnell and passes a structured object containing positive prompt, negative prompt, resolution, guidance scale, seed, and gallery metadata.

Before generation it assembles the positive prompt from separate layers: selected style, selected theme/attributes, free-form user text, and a structured description assembled from UI fields. A selected style may also carry a hidden negative suffix. The UI exposes square, portrait, and landscape resolutions; guidance scale 1–30 with 7 as the visible default; optional seed; and large 4/8/16/32 batches.

Its optional text assistant follows a useful rule: fuse raw traits into one primary subject, describe only explicitly supplied visual facts, avoid filling absent categories, and return one concise natural-language paragraph. This is more reliable for Flux than a long unordered tag dump.

The generator also recognizes inline control tokens such as `(resolution:::)`, `(seed:::)`, `(guidanceScale:::)`, and `(negativePrompt:::)`. Those tokens can override UI/provider parameters, so accepting them from an LLM would weaken immutable generation provenance.

## Adopted here

- natural-language, single-paragraph Visual Director prompts with ordered subject → action → appearance → environment → light → camera layers;
- explicit character-trait fusion and a strict no-hallucinated-decoration rule;
- a separate reusable style layer and separate style-specific negative layer;
- stronger composition, background integration, lighting, anatomy, hand, and artifact constraints;
- server-side stripping of all reserved inline control tokens before prompt persistence;
- browser-worker validation for resolution, optional guidance scale, optional seed, and exact variant count;
- prompt-profile migration `00021_perchance_prompt_profile_v2.sql` so database defaults match application defaults.

## Intentionally not copied

- the custom `urv-image-plugin` dependency: the project keeps the narrower `text-to-image-plugin` `generateImage()` boundary used by its local mailbox worker;
- the generator's very large character/wardrobe/NSFW taxonomy: Story Canon and the Visual Bible are the authoritative source here;
- 4–32 image batches: the product contract remains exactly two variants to bound latency, storage, and worker result size;
- inline provider-control tokens in prompts: parameters must remain explicit, validated provenance rather than model-controlled text.

No third-party generator code or prompt catalog was vendored. Only the architectural ideas above were adapted to this project's story-specific constraints.
