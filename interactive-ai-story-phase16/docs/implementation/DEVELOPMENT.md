# Development reference

This document is the current reference for local verification, CI, environment
variables, and database migrations. `RUN_LOCAL.md` remains the guided first-run
path.

## Environment variables

Backend runtime:

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_ENV` | `development` | Logging/runtime environment label. |
| `HTTP_ADDR` | `127.0.0.1:8787` | Backend listen address. |
| `DATABASE_URL` | empty | PostgreSQL DSN. The full application is enabled when it is set. |
| `LOCAL_OWNER_ID` | fixed local UUID | Owner bootstrapped for the local single-user application. |
| `LOCAL_USERNAME` | `local` | Local user name. |
| `STORY_LLM_BASE_URL` | `http://127.0.0.1:8081` | OpenAI-compatible Story LLM endpoint. |
| `STORY_LLM_API_KEY` | empty | Optional bootstrap credential. Do not commit it. |
| `STORY_LLM_MODEL` | project baseline model | Bootstrap model identity. |
| `STORY_LLM_PROFILE` | `llamacpp-16k-kvq8` | Bootstrap model profile. |
| `SECRET_STORE_PATH` | `../data/secrets/provider-keys.json` | Owner-only provider credential store. |
| `OMNIVOICE_BASE_URL` | `http://127.0.0.1:6655` | Local narration service endpoint. |
| `MEDIA_ROOT` | `./data/generated` | Generated image storage root. |
| `MEDIA_BASE_URL` | `/media` | Public path for generated images. |
| `IMAGE_WORKER_TOKEN` | empty | Optional Bearer token for every `/internal/image-worker/*` route. |
| `EMBEDDING_BASE_URL` | `http://127.0.0.1:8090` | Local sentence-transformers endpoint. |
| `EMBEDDING_MODEL` | `deepvk/USER2-small` | Expected embedding model identity. |

Generation switches accept `1`, `true`, `yes`, or `on`:

- `GENERATION_CONTEXT_SHADOW_V1`
- `GENERATION_TURN_PLANNER_V1`
- `GENERATION_PROVISIONAL_BEAT_V1`
- `GENERATION_JOURNAL_EVIDENCE_ISOLATION_V1`
- `GENERATION_FAST_STATE_V1`

Compose and local launcher variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `POSTGRES_DB` / `POSTGRES_USER` / `POSTGRES_PASSWORD` | `story` | Shared Compose database credentials used by PostgreSQL, migrations, and backend DSN construction. |
| `BACKEND_PORT` / `FRONTEND_PORT` | `8787` / `5173` | Loopback-published Compose ports. |
| `VITE_API_BASE_URL` | empty | Browser API origin; empty uses same-origin proxying. |
| `VITE_DEV_BACKEND_TARGET` | `http://127.0.0.1:8787` outside Compose | Vite development proxy target. |
| `HOST_DATABASE_URL` | local PostgreSQL DSN | Shared `make migrate-up`, `make migrate-down`, and `make backend-dev` DSN. |
| `HOST_STORY_LLM_BASE_URL` | `http://127.0.0.1:8081` | Host launcher Story LLM endpoint. |
| `HOST_HTTP_ADDR` | `127.0.0.1:8787` | Host launcher backend address. |
| `HOST_FRONTEND_HOST` | `0.0.0.0` | Vite bind host used by `make frontend-dev`. |
| `HOST_MEDIA_ROOT` | `../data/generated` | Host launcher media directory. |
| `HOST_SECRET_STORE_PATH` | `../data/secrets/provider-keys.json` | Host launcher secret store. |

Never commit real API keys, database passwords, worker tokens, or the generated
provider secret store. `.env.example` contains non-secret examples only.

## Verification

### Optional story memory

Apply migrations through 00040 before using memory. In AI Settings, enable story
memory, choose top-k (1–100, default 8) and estimated memory tokens (256–8192,
default 1024), then save a new revision. Older revisions default to disabled.
`memoryEnabled`, `memoryTopK`, and `memoryTokenBudget` are public revision fields.
The embedding endpoint uses the revision's `embeddingEndpoint`, falling back to
`EMBEDDING_BASE_URL`. Memory currently supports `local_sentence_transformers`.

`GET /api/v1/settings/memory` reports preparation/progress/failure and the serving
revision. `POST /api/v1/settings/memory/reindex` accepts an empty JSON object,
requires memory enabled, and requests a new background build. Concurrent requests
reuse the current build. The old index remains serving until the new build
finishes. A failed rebuild leaves the old serving profile in place.

The controller polls settings; disabled memory makes no model calls. A setting
change is observed at the next controller cycle and does not cancel an already
running indexing attempt. Retrieval stays bounded and optional. See
[`MEMORY_RUNTIME_SETTINGS_2026-11-09.md`](MEMORY_RUNTIME_SETTINGS_2026-11-09.md)
for setup, scope, and verification limitations.

### Standard checks

Backend:

```bash
cd backend
test -z "$(gofmt -l .)"
go vet ./...
go test ./...
go test -race ./...
cd ..
```

Embedding provider contract (does not download the model):

```bash
python3 -m unittest discover -s tools/embedding_provider -p 'test_app.py'
```

Frontend:

```bash
cd frontend
npm ci
npm run typecheck
npm run lint
npm test -- --run
npm run build
cd ..
```

PostgreSQL integration:

```bash
docker compose -f deploy/docker-compose.yml up -d postgres
make migrate-up
cd backend
DATABASE_URL='postgres://story:story@127.0.0.1:5432/story?sslmode=disable' \
  go test ./tests/postgres -count=1
cd ..
```

Production frontend image:

```bash
docker build -f frontend/Dockerfile --target runtime -t interactive-story-frontend:local frontend
docker run --rm interactive-story-frontend:local nginx -t
```

`npm ci` currently reports dependency audit findings. Do not apply
`npm audit fix --force` without reviewing breaking dependency changes.

## Continuous integration

`.github/workflows/ci.yml` runs on pushes and pull requests:

1. backend format, vet, unit/integration-independent tests, race tests, and the
   embedding provider contract;
2. frontend install from `package-lock.json`, typecheck, lint, tests, and build
   on Node.js 22;
3. a `pgvector/pgvector:pg16` service, all Goose migrations, migration status,
   and the PostgreSQL integration suite.

The workflow uses read-only repository permissions and cancels superseded runs
for the same branch/ref.

## Migration inventory

Migrations are append-only. Never edit an already applied migration. The next
migration after this inventory must be `00041_*`.

```text
00001_extensions.sql
00002_users_stories.sql
00003_timelines_events.sql
00004_snapshots.sql
00005_narrative_state.sql
00006_savepoints_branching.sql
00007_ai_provider_framework.sql
00008_vector_memory.sql
00009_phase8_architecture_corrections.sql
00010_story_setup.sql
00011_director_mode.sql
00012_save_library.sql
00013_image_generation.sql
00014_ai_provider_settings.sql
00015_generation_jobs_reliability.sql
00016_generation_updates.sql
00017_perchance_image_generation.sql
00018_image_generation_moments.sql
00019_image_generation_paragraph_anchor.sql
00020_runtime_prompt_studio.sql
00021_perchance_prompt_profile_v2.sql
00022_hierarchical_quests.sql
00023_initial_quests.sql
00024_generation_active_head_guard.sql
00025_setup_generation_guard.sql
00026_quest_types.sql
00027_director_world_editor.sql
00028_durable_image_prompt_jobs.sql
00029_world_rules.sql
00030_generation_workflow_milestone.sql
00031_generation_run_metrics.sql
00032_context_shadow_metrics.sql
00033_choices_fallback_metrics.sql
00034_action_intent_fallback_metrics.sql
00035_provisional_generation_updates.sql
00036_perchance_image_dimensions.sql
00037_memory_index_outbox.sql
00038_memory_index_publication.sql
00039_memory_russian_search.sql
00040_memory_runtime_profiles.sql
```
