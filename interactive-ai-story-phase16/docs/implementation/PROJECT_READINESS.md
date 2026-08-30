# Project readiness

## Verdict

**Ready for a local MVP launch attempt:** yes.

**Ready to call production/release PASS:** no.

The implemented playable path is sufficiently connected to try on the target machine:

1. PostgreSQL/pgvector migrations
2. Story Setup through real OpenAI-compatible llama.cpp
3. Main Timeline / Chapter / Scene / Beat
4. Reader choices through durable GenerationJobs
5. SSE generation updates/reconnect history
6. Saves/Forks
7. Director exact edits/instructions
8. Perchance browser-worker Scene illustrations
9. two persisted 768×768 variants + selected image
10. local media serving

## Checks passed in the 2026-08-23 audit

- `go test ./...`
- `go vet ./...`
- Go server build
- PostgreSQL/pgvector migrations `00001..00021` on a fresh temporary database
- PostgreSQL Canon/Save/Fork integration tests against that fresh database
- frontend Vitest suite
- frontend TypeScript check, zero-warning ESLint gate, and production Vite build
- npm audit, including development dependencies: no known vulnerabilities reported
- Go `govulncheck`: no reachable vulnerabilities after upgrading `pgx` and `x/text`
- full Go race-detector suite
- Story Setup tests
- gameplay generation tests
- durable jobs/lease/retry/cancel tests
- save/fork tests
- Director tests
- semantic replay tests
- Perchance image generation service tests
- exactly-two-images validation
- JPEG/PNG/WebP signature validation
- result idempotency
- local storage path safety
- versioned real Story LLM prompt contract tests
- Violentmonkey JavaScript syntax
- Perchance runtime worker and Violentmonkey JavaScript syntax
- Docker Compose YAML parse
- read-only inspection of the live `urvillain-imagine` UI and current prompt assembly
- local backend live/ready health smoke and browser smoke for New Story, AI Settings, and Prompt Studio with no console warnings/errors

## Why release PASS is still withheld

Still not executed end to end:

- the selected GGUF through llama.cpp;
- a real image job travelling through backend → Violentmonkey → Perchance `generateImage()` → local storage;
- browser/mobile E2E for the complete playable flow;
- multi-worker kill/restart recovery under load.

These are runtime verification gaps, not intentionally hidden PASSes.

## Known implementation gaps that do not block the first local launch attempt

1. ContextBuilder/vector retrieval infrastructure exists but is not yet wired into the live generation pipeline. The current real LLM roles now use their versioned prompt contracts, but long-running story coherence will still need runtime ContextBuilder integration.
2. Vector-memory ingestion/reindex infrastructure exists, but live memory ingestion is not yet complete.
3. Director instruction create/complete/expire lifecycle is not fully represented by semantic replay events.
4. Prometheus image-generation metrics are not yet exported.
5. Image cleanup/retention and S3/MinIO storage are not implemented; local storage is the MVP baseline.
6. The Perchance browser provider is intentionally a local MVP provider and depends on an open browser tab.
7. Full PostgreSQL replay equality, multi-worker kill/restart, and browser mobile E2E remain external acceptance gates.

## First-launch acceptance checklist

Do not call the local run successful until all are true:

- [x] migrations `00001..00021` apply to a fresh database;
- [ ] backend `/health/ready` is 200;
- [ ] llama.cpp `/v1/models` responds;
- [ ] New Story → Setup generation succeeds with the real model;
- [ ] Start creates the Reader opening;
- [ ] Reader choice produces the next Beat;
- [ ] restart/reconnect does not lose a durable generation;
- [ ] save + safe fork works;
- [ ] Director stat edit survives reload;
- [ ] `/internal/image-worker/status` becomes connected;
- [ ] initial Scene automatically queues one image generation;
- [ ] Perchance returns exactly two 768×768 images;
- [ ] files appear under `data/generated`;
- [ ] both variants render in Reader;
- [ ] selecting an image survives reload;
- [ ] Regenerate creates a new generation rather than overwriting the old one;
- [ ] closing the Perchance tab causes lease/retry behavior rather than damaging Story Canon.
