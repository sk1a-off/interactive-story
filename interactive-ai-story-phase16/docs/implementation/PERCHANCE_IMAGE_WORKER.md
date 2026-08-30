# Perchance browser image worker

Current image baseline:

- provider: `perchance_browser`
- Perchance plugin: `text-to-image-plugin`
- style: Digital Painting
- resolution: 768×768
- variants: 2
- active browser jobs: 1
- parallel images inside one job: 2
- lease: 5 minutes
- max attempts: 3
- result body limit: 40 MiB
- accepted images: JPEG, PNG, WebP

Prompt assembly is deliberately layered:

1. the Visual Director writes one factual English scene paragraph;
2. the generation stores a frozen Digital Painting style prompt;
3. visual-bible and hard artifact/anatomy negatives are merged separately;
4. the browser worker trims and joins the layers without empty commas;
5. reserved Perchance inline control tokens are removed from LLM output before a job is stored.

The worker accepts validated `resolution`, `guidanceScale`, and `seed` fields for forward compatibility, but the current server contract remains two independent 768×768 variants with provider-random seeds.

Successful image data is cached in the open runtime long enough to replay the same leased job after a temporary localhost upload failure. Provider failures are not cached, so the backend retry can genuinely call `generateImage()` again. Local result uploads use three bounded retries before releasing the browser-side job back to the durable backend lease.

The design comparison with `urvillain-imagine` is documented in `URVILLAIN_PROMPT_RESEARCH.md`.

## Runtime topology

`Go/PostgreSQL -> internal worker API -> Violentmonkey -> scoped DOM mailbox -> Perchance generateImage() -> scoped DOM mailbox -> Violentmonkey -> Go -> local ImageStorage`

Violentmonkey is transport only. It contains no story logic and never calls `generateImage()`. It is restricted to the dedicated `/interactive-ai-story-worker` generator path and does not broadcast prompts or image results through wildcard `postMessage`.

## Files

- `tools/perchance/violentmonkey.user.js` — install into Violentmonkey.
- `tools/perchance/perchance-runtime-worker.html` — paste into a Perchance generator named exactly `interactive-ai-story-worker`, in the HTML/runtime that has `generateImage = {import:text-to-image-plugin}` available.

No CAPTCHA/Turnstile bypass is implemented. If Perchance requests manual verification, complete it in the open Perchance tab.

## Worker endpoints

- `GET /internal/image-worker/jobs/next`
- `POST /internal/image-worker/jobs/{id}/result`
- `POST /internal/image-worker/jobs/{id}/failed`
- `POST /internal/image-worker/heartbeat`
- `GET /internal/image-worker/status`

The backend is bound to loopback by default (`127.0.0.1:8787`).

## Public endpoints

- `POST /api/v1/stories/{storyID}/scenes/{sceneID}/images`
- `GET /api/v1/image-generations/{generationID}`
- `PUT /api/v1/scenes/{sceneID}/selected-image`

A new generation never overwrites an older generation.

## Storage

Base64/data URLs are transport-only and are never written to PostgreSQL. The backend validates MIME + file signatures, decodes bytes, writes them through `ImageStorage`, then stores only URL/content metadata in `scene_images`.

Default local storage:

- root: `./data/generated` when backend runs on host
- URL prefix: `/media`
- layout: `stories/{storyID}/scenes/{sceneID}/{generationID}/{variant}.{ext}`

## Automatic scheduling

Image generation is scheduled only after the Scene is durably committed:

- initial Story Setup scene: scheduled after `StartStory` commits;
- future semantic `scene_started`: post-commit observer schedules it;
- scheduling failure never rolls back Story Canon.

Manual Regenerate is also supported from Reader and creates a new ImageGeneration.
