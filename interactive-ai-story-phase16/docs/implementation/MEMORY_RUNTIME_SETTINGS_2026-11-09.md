# Runtime memory settings and versioned reindexing — 2026-11-09

Memory is now wired into server startup and the optional role/image hooks.
It remains disabled by default. This work changes code and an isolated test
database; it does not migrate the application's live database, activate its AI
revision, or claim real-model acceptance.

## Settings and lookup

Public AI revision fields are `memoryEnabled` (false), `memoryTopK` (8, range
1–100), and `memoryTokenBudget` (1024 estimated tokens, range 256–8192). Old
revisions without these fields stay disabled. Saving creates a new immutable
revision; queued story jobs resolve memory using their pinned AI revision.
Changing only text/image settings or retrieval limits does not re-embed history.

The embedding identity includes local owner, provider, model, and effective
endpoint. Revision `embeddingEndpoint` takes precedence over the environment
fallback `EMBEDDING_BASE_URL`. Newly enabled memory supports the existing local
sentence-transformers provider. The server uses the completed serving profile
for this identity; a model/endpoint change cannot select the old identity's
index. It bounds serving-profile lookup to 200 ms and retrieval to one second.
These are time limits, not measured latency improvements.

The image context factory selects text LLM, prompt revision, and memory from one
AI revision. Single/moment/paragraph requests and their repairs/fallbacks keep
that selection. The existing source-beat event checkpoint remains the memory
visibility boundary.

## Index lifecycle

Migration 00040 adds owner-scoped build/serving profiles and revision-scoped
outbox jobs. Existing NULL-profile jobs become a durable source catalog and
retain their legacy queue behavior. Per-profile uniqueness allows the same Canon
source to be embedded into separate revisions. Claims, predecessor ordering, and
lease recovery are partitioned by index revision. Publication verifies both the
job's revision and its stored provider/model; retired profiles cannot claim work.

The background controller checks the active AI settings. When enabled, it seeds
the local owner's pre-outbox events/snapshots, creates a build, and supervises
bounded indexing attempts. There are no embedding calls in the Canon transaction
or HTTP reindex request. Existing retry/lease recovery survives restarts. Model
errors are logged generically; no provider response/source text is logged.

Canon outbox insertion fans out to serving and building profiles belonging to
that story's owner. Creation, fanout, and promotion share a short transaction
advisory lock. The installed PostgreSQL unit of work uses default READ COMMITTED;
concurrent Canon commit/build tests exercise the source-seeding boundary.

Existing `embedding_reindex_jobs` tracks each build. Explicit rebuild requests
reuse an already building revision, otherwise allocate a fresh index revision.
The previous serving revision continues receiving new work while the new one
builds. No unfinished profile is selected for prompts. Only after all live-story
jobs complete does one transaction retire the old profile and promote the new
one. A failed source is retried automatically (see "Failure recovery" below);
only an exhausted source fails the build, and never removes the old serving profile.
Publication lag still hides stale structured state. Missing/corrupt source
history fails closed rather than inventing a successful index.

Disabling memory stops new controller work after the current cycle and omits
memory from newly selected disabled revisions. It preserves derived data and
does not cancel an already running attempt. Re-enabling resumes queued work.
Retired/failed vectors and source catalog rows are retained; automatic retention
or cleanup is not part of this increment. Rollback of 00040 deletes profile work
and metadata while preserving Canon, the source catalog, and vector rows.

## Interface and API

AI Settings includes enablement, retrieval limits, progress, readiness/failure,
and a reindex button. Unsaved/disabled settings and an existing build disable
the button. Failure to refresh the status does not claim readiness. Pending
incremental indexing and failed serving jobs have distinct updating/degraded
states rather than falsely displaying ready.

- `GET /api/v1/settings/memory`: enabled/state, build and serving revisions,
  queued/running/completed/failed job counts for the local owner, plus
  `exhausted` (failed jobs that used every automatic recovery).
- `POST /api/v1/settings/memory/reindex`, JSON `{}`: 202 when requested; 422 if
  disabled; generic 503 for unavailable runtime/storage. Malformed JSON and
  non-JSON bodies are rejected before performing work.

## Executed verification

- Baseline full backend/vet/test/race with PostgreSQL and frontend checks passed.
- 00040 up/down/up passed against `story_image_settings_00036_check`; the live
  application database and its settings were untouched.
- Runtime unit tests: disabled mode performs no model/index work, settings and
  identity mapping, serving-only lookup, unavailable-index fallback, explicit
  rebuild, and shutdown cancellation. Domain tests verify defaults and ranges.
- PostgreSQL: full event → document embedding → completed profile → hybrid search;
  incomplete build exclusion; old serving retention; profile-specific fanout and
  queue ordering; stale-state fencing; wrong-model publication rejection; failed
  rebuild retention; owner isolation (including database rejection of assigning
  another owner's source to a profile); receipt replay after retirement. Two concurrent build requests plus a Canon
  commit produce one build and exactly one task for that source. These runtime
  integration scenarios passed ten repetitions with race detection.
- HTTP tests cover status/reindex, disabled rejection, invalid input, and generic
  errors. Frontend tests cover defaults/toggle, unsaved changes, JSON rebuild,
  progress, and duplicate-build prevention. Existing image fallback tests also
  verify one pinned context factory call.

Final full Go formatting/vet/test/race with PostgreSQL and frontend
install/typecheck/lint/test/build passed (45 frontend tests). The existing 14
frontend dependency audit findings remain; no dependencies were changed.

## Local activation and remaining acceptance

The live probe `http://127.0.0.1:8090/health` refused connection during this work.
Real USER2 inference, browser visual acceptance, real-story reindexing, frozen
recall@k, and end-to-end timing are not claimed. All embedding tests use a fake
provider. To prepare the existing local service from the project root:

```bash
python3 -m venv /tmp/story-embedding-venv
/tmp/story-embedding-venv/bin/pip install -r tools/embedding_provider/requirements.txt
/tmp/story-embedding-venv/bin/uvicorn tools.embedding_provider.app:app --host 127.0.0.1 --port 8090
```

This downloads/loads `deepvk/USER2-small` on CPU. Check `/health` reports that
model and 384 dimensions. Apply migrations with `make migrate-up`, restart the
backend/frontend, and enable memory through AI Settings when ready to exercise
the real service. Next: frozen-corpus recall/search-latency benchmarking before
claiming memory quality or response-speed improvements.

## Failure recovery (2026-11-12)

A job that exhausts its five attempts no longer blocks its timeline forever.
Each runtime tick requeues failed jobs whose cooldown has elapsed; the cooldown
escalates with the job's `recoveries` counter (1, 2, 4, … minutes, capped at one
hour) and recovery stops after `MaxMemoryIndexRecoveries` (8). No source event
is skipped: the same job is processed again in outbox order, and later jobs of
that timeline wait behind it as before. The status reports such jobs as
`failed` (state `degraded`) while they wait, and counts jobs that gave up as
`exhausted`; those need a manual reindex. A building profile stays `building`
while its failed jobs are still recoverable and turns `failed` only when one is
exhausted. Recoveries are logged as warnings with the affected revision and
job count. Migration `00042_memory_index_recovery.sql` adds the counter.
