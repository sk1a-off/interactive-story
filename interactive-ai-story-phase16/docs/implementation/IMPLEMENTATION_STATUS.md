# Implementation Status

## Documentation baseline

- Specification bundle: **v1.12**
- Authoritative implementation plan: `67_MASTER_IMPLEMENTATION_PLAN_v1_12.md`
- Acceptance matrix: `69_acceptance_gates_and_test_matrix.md`
- Current implementation phase: **Phase 16 plus post-phase hardening — multi-moment anchors, Prompt Studio, and Perchance prompt profile v2**
- Current architecture registry: `00_current_architecture_decisions.md`

## Current verification snapshot — 2026-08-23

This snapshot supersedes the older sandbox-availability notes retained later in the phase history:

- full `go test ./...`, `go vet ./...`, and Go build pass;
- full frontend test/typecheck/zero-warning lint/production build pass;
- Go race-detector and `govulncheck` pass after upgrading vulnerable `pgx`/`x/text` releases;
- Docker PostgreSQL/pgvector is available;
- migrations `00001..00021` pass on a fresh temporary database;
- PostgreSQL Canon/Save/Fork integration tests pass;
- local backend health and browser smoke for New Story, AI Settings, and Prompt Studio pass without console warnings/errors;
- the live `urvillain-imagine` UI and current source-level prompt assembly were inspected read-only;
- real llama.cpp story generation, a complete Violentmonkey image round trip, mobile browser E2E, and multi-worker restart/load gates remain unverified.

## Phase status

| Phase | Status | Notes |
|---|---|---|
| Phase 0 — Repository/toolchain | PARTIAL | implementation complete; Docker/npm/module-registry runtime verification blocked by execution environment |
| Phase 1 — PostgreSQL/Event/Timeline foundation | PARTIAL | deterministic core complete; PostgreSQL integration test prepared but cannot execute locally without PostgreSQL/dependencies |
| Phase 2 — Narrative/state domain | PARTIAL | deterministic invariants complete; DB migration/runtime verification blocked locally |
| Phase 3 — Saves/forks/restore | PARTIAL | deterministic branch semantics pass; PostgreSQL materialization test prepared, runtime execution blocked locally |
| Phase 4 — Provider framework | PARTIAL | provider ports/registry, immutable config/provenance model and deterministic Fake providers implemented; PostgreSQL migration runtime remains unverified |
| Phase 5 — Fake-AI playable vertical slice | PARTIAL | deterministic role pipeline, validation/policy/atomic Canon boundary, idempotent submission and normalized SSE hub implemented; DB/browser E2E runtime remains unverified |
| Phase 6 — Real Story LLM integration | PARTIAL | OpenAI-compatible StoryLLM adapter, versioned prompt bundle and bounded structured repair implemented; real baseline model runtime benchmark/integration cannot execute in this sandbox |
| Phase 7 — Context Builder / vector memory | PARTIAL | authoritative-first Context Builder, embedding port integration, filtered pgvector retrieval, budget policy and reindex schema implemented; real PostgreSQL/pgvector execution remains unverified |
| Phase 8 — Story creation/setup | PARTIAL | New Story/setup generation, component locks/manual edits/regeneration, transactional Story start, first Reader Beat and responsive UI implemented; full browser/PostgreSQL/real-LLM E2E remains unverified |
| Phase 9 — Director Mode | PARTIAL | typed exact state edits, safety SavePoint, semantic Director event, audit history, hard/soft instructions and mobile full-screen Director implemented; PostgreSQL/browser/replay gate remains unverified |
| Phase 10 — Full save/branch UX | PARTIAL | Save Library aggregate/API, save cards/slots metadata, branch ancestry, safe-fork confirmation and phone-first Timeline UI implemented; real PostgreSQL/browser Gate remains unverified |
| Phase 11 — Images | PARTIAL | provider-neutral async image assets, immutable provenance, failure isolation and responsive Reader placeholders implemented; real image runtime/PostgreSQL/browser gate remains unverified |
| Phase 12 — AI Provider Settings | PARTIAL | immutable config revisions, active pointer, revision-scoped write-only secrets, new-job provider pinning and mobile settings UX implemented; PostgreSQL/browser runtime gate remains unverified |
| Phase 13 — Reliability / durable jobs | PARTIAL | PostgreSQL-backed generation queue, persisted request payload/idempotency, SKIP LOCKED claim/lease, retry/cancel/timeout and restart recovery implemented; real multi-worker/PostgreSQL gate remains unverified |
| Phase 14 — Semantic replay | PARTIAL | strict typed event reducers, live Canon projection materialization, reconstructable opening/gameplay Beats and Director exact-edit replay implemented; PostgreSQL replay equality gate remains unverified |
| Phase 15 — SSE reconnect + image endpoint | PARTIAL | persisted generation update history with monotonic sequence/Last-Event-ID replay and one explicit image-generation endpoint implemented; PostgreSQL/browser/runtime gates remain unverified |
| Phase 16 — Perchance browser image generation | PARTIAL | durable Scene ImageGeneration queue, lease/retry, Violentmonkey+DOM worker, 2×768² storage/selection/regeneration, auto post-Scene scheduling and launch documentation implemented; real PostgreSQL/llama.cpp/browser/Perchance E2E remains unverified |

No phase is marked PASS while required external-runtime checks have not actually run.

## Implemented through Phase 3

### Canon / Timeline foundation

- strongly typed Story/Timeline/Event/Snapshot IDs;
- append-only semantic event sequence;
- expected-head stale-write rejection;
- transaction/UoW boundary;
- replay and snapshots;
- snapshot hash verification;
- snapshot schema version **2** includes implemented narrative projection state;
- rebuild supports fork base Snapshot @ `N` followed by child-local events starting at `N+1`.

### Narrative/state domain

- Chapters / Scenes / Beats;
- Character definitions and Timeline CharacterState;
- extensible Stats, including target/evolution metadata;
- directed Relationships;
- Items / ItemState with exclusive owner/location rule;
- Facts;
- character-specific Knowledge;
- Beliefs independent from Facts/Knowledge;
- Story Threads;
- Director Instructions;
- narrative projection clone/fork helpers.

### SavePoint / branching

- immutable SavePoint canonical reference;
- snapshot-on-save;
- automatic capture of current Chapter / Scene / active Beat cursor;
- preview from exact saved Snapshot;
- safe fork from SavePoint;
- MVP restore policy: default safe fork; destructive in-place restore is explicitly rejected as Advanced workflow rather than silently rewinding append-only history;
- Timeline library query/service;
- SavePoint list query/service;
- child Timeline lineage metadata (`parent_timeline_id`, `forked_from_save_id`);
- child semantic head remains source Save sequence `N`;
- fork creation does **not** append a synthetic semantic event;
- first child semantic event receives `N+1`;
- full implemented narrative snapshot is remapped/materialized into child projection;
- Timeline-local primary-key identities are regenerated on fork (Chapter/Scene/Beat, Relationship, Fact, Belief, Thread, Director Instruction);
- Story-global Character and Item identities remain shared;
- dependent references are remapped (Scene→Chapter, Beat→Scene, Knowledge→Fact, relationship/scene-owned Stats).

## Database integrity added

### `00005_narrative_state.sql`

- narrative/state tables and constraints;
- directed relationship uniqueness;
- active Beat supersession uniqueness;
- Fact/Knowledge/Belief separation;
- Timeline/Story scope triggers;
- item ownership/location exclusivity.

### `00006_savepoints_branching.sql`

- `save_points` table;
- exact composite SavePoint→Snapshot FK `(snapshot_id, timeline_id, event_seq)`;
- same-Timeline head Snapshot FK;
- same-Story parent Timeline FK;
- same-Story fork-origin SavePoint FK;
- SavePoint cursor scope trigger for Chapter/Scene/Beat;
- immutable SavePoint canonical-reference trigger (rename/note/pin remain mutable in future PATCH implementation);
- immutable Timeline lineage trigger;
- fork lineage must match parent SavePoint and initial head sequence;
- FK protection prevents deletion of a SavePoint still used as branch origin.

## API surface implemented in Phase 3

- `GET /stories/{storyId}/timelines`
- `GET /timelines/{timelineId}/saves`
- `POST /timelines/{timelineId}/saves`
- `POST /timelines/{timelineId}/fork`
- `POST /saves/{saveId}/restore`
  - `mode=preview`
  - `mode=fork`
  - `mode=restore` returns explicit Advanced-workflow conflict in MVP
- existing `GET /health/live`
- existing `GET /health/ready`

Transport DTOs are separate from domain structs and use API-contract camelCase fields.

## Tests executed successfully in this environment

- `go test ./internal/domain/... ./internal/application/...`
- `go vet ./internal/domain/... ./internal/application/... ./internal/config ./internal/observability`
- `gofmt` check
- `git diff --check`

Covered deterministic cases include:

- stale expected head rejected without mutation;
- contiguous replay;
- snapshot tamper detection;
- snapshot/replay equality;
- Fact/Knowledge/Belief separation;
- Knowledge requires active Fact;
- relationship direction and self-reference rejection;
- item owner/location exclusivity;
- cross-Timeline domain-state rejection;
- SavePoint exact event sequence;
- SavePoint automatic chapter/scene/beat cursor capture;
- preview exact snapshot;
- fork child head equals source Save sequence;
- first child event is `N+1`;
- parent/child divergence;
- sibling event isolation;
- restart-style rebuild from fork Snapshot + child-local events;
- narrative projection fork ID remapping;
- Knowledge→Fact and Stat→Relationship remapping;
- parent projection remains unchanged after child materialization.

## PostgreSQL integration coverage prepared

`backend/tests/postgres/canon_integration_test.go` now covers:

- append / stale head / replay / snapshot;
- SavePoint exact sequence;
- fork lineage;
- parent-only event does not leak to child;
- child narrative Chapter/Scene/Beat projection is materialized from Snapshot;
- Timeline-local UUIDs are remapped rather than reused;
- cross-Timeline SavePoint→Snapshot reference is rejected by PostgreSQL.

CI migration smoke applies all migrations against `pgvector/pgvector:pg16` before running PostgreSQL tests.


## Implemented in Phase 4

- provider-neutral `StoryLLM`, `EmbeddingProvider`, and `ImageProvider` ports;
- provider identity is `(kind, provider, model, profile)` and is resolved through a concurrency-safe registry;
- deterministic `FakeStoryLLM`, `FakeEmbeddingProvider`, and `FakeImageProvider`;
- immutable AI configuration revision domain model;
- generation jobs pin the selected config revision and provider identity at creation time, so later provider switching cannot mutate an already-created job;
- normalized generation lifecycle (`queued`, `running`, `completed`, `failed`, `cancelled`) independent of provider-specific statuses;
- migration `00007_ai_provider_framework.sql` adds `ai_config_revisions`, `generation_jobs`, and append-only `generation_attempts`;
- PostgreSQL triggers prevent mutation of generation provenance;
- no real Story LLM, image runtime, or embedding runtime is introduced in this phase.

### Phase 4 tests executed

- `go test ./internal/domain/... ./internal/application/... ./internal/adapters/fakeai ./internal/ports/...` — PASS;
- `go vet ./internal/domain/... ./internal/application/... ./internal/adapters/fakeai ./internal/ports/...` — PASS;
- fake embedding and image outputs are deterministic;
- unknown provider identity does not silently fall back;
- invalid provider configuration is rejected;
- generation job retains its originally pinned provider/model after a later configuration object is changed;
- invalid generation lifecycle transitions are rejected.

Phase 4 remains **PARTIAL** because migration `00007` and its DB immutability triggers cannot be executed against a real PostgreSQL instance in this sandbox.


## Implemented in Phase 5

- deterministic role pipeline: Action Interpreter → Director → Writer → Objective Evaluator → Choices;
- typed role inputs and JSON outputs at the application boundary;
- malformed role output is rejected before Canon mutation;
- application policy converts validated proposals into semantic events; Fake/LLM providers never write the database;
- atomic semantic batch contains `player_action_attempted`, `beat_committed`, and `choices_ready`, each with generation provenance;
- expected-head is checked at the Canon transaction boundary, so a stale generation cannot partially commit;
- normalized generation phases independent of provider status;
- in-process non-blocking generation update hub suitable for SSE;
- action submission route and generation SSE route are registered when a generation application is wired;
- request idempotency key maps duplicate submissions to the same generation in the Phase 5 runner;
- Fake role fixtures are deterministic and real LLM integration remains absent.

### Phase 5 tests executed

- deterministic role pipeline success — PASS;
- malformed LLM JSON causes zero Canon mutation — PASS;
- stale expected head causes zero partial commit — PASS;
- all committed semantic events carry generation provenance — PASS;
- duplicate request key returns the same generation ID — PASS;
- slow SSE subscriber cannot block generation — PASS;
- domain/application regression suite — PASS;
- `go vet` for Phase 5 deterministic packages — PASS.

### Phase 5 remaining gate work

Phase 5 is not marked PASS because the authoritative vertical-slice gate requires a real browser/API/PostgreSQL/Fake-AI run. This sandbox still cannot compile the pgx/chi transport path without external module downloads and has no PostgreSQL/Docker runtime. The process-local runner is intentionally not presented as the final durable worker/jobs subsystem; persistence/restart recovery remains a later jobs/reliability responsibility.

## Implemented in Phase 6

- OpenAI-compatible `StoryLLM` adapter behind the existing provider port;
- documented baseline model/runtime is represented only in environment configuration, not domain/application business logic;
- adapter requests JSON-object output, applies HTTP timeout/cancellation, limits response body size, maps transport/provider failures to provider-neutral errors, and reports token usage;
- API keys are sent only as authorization headers and are not placed in request payloads;
- versioned prompt bundle under `backend/prompts/v1` for Action Interpreter, Director, Writer, Choices and Structured Repair;
- bounded controlled structured repair in the application pipeline; exhausted repair budget fails the generation without Canon mutation;
- no hidden chain-of-thought is requested or stored.

### Phase 6 tests executed

- OpenAI-compatible adapter contract using an in-memory HTTP transport — PASS;
- provider identity/model pinning — PASS;
- secret does not leak into request body — PASS;
- non-JSON assistant output rejected — PASS;
- one bounded repair can recover malformed structured output — PASS;
- exhausted repair budget leaves Canon untouched — PASS;
- Phase 5 deterministic pipeline regression suite — PASS;
- `go vet` for Phase 6 dependency-free packages — PASS.

Phase 6 remains **PARTIAL**. The actual documented Vikhr/llama.cpp baseline cannot be launched in this sandbox, and the preceding authoritative Fake-AI browser/PostgreSQL Gate is still externally unverified. The adapter is therefore implemented and contract-tested, but no claim is made that the real model quality/runtime gate has passed.

## Implemented in Phase 7

- standalone Context Builder subsystem with authoritative current state, recent Beats, summaries and semantic retrieval as distinct priority classes;
- deterministic budget policy that never drops authoritative current state in favor of retrieval;
- EmbeddingProvider integration remains behind the existing provider port;
- memory retrieval port requires Story, Timeline, owner and event-validity scope;
- PostgreSQL pgvector adapter applies those predicates in SQL before vector-distance ordering;
- application layer defensively rechecks returned memory scope to prevent leakage from a faulty adapter;
- migration `00008_vector_memory.sql` adds versioned embeddings, HNSW cosine index, temporal validity, provider/model/revision provenance and embedding reindex jobs;
- embedding provider/model switching has an explicit revision/reindex path rather than silently reinterpreting old vectors.

### Phase 7 tests executed

- authoritative state survives exhausted context budget — PASS;
- valid same-Timeline/owner memory is retrieved — PASS;
- sibling Timeline memory is rejected defensively — PASS;
- expired memory is rejected — PASS;
- retrieval query receives Timeline/owner/head filters — PASS;
- malformed embedding shape is rejected by the Context Builder contract;
- Phase 5/6 deterministic regression packages — PASS;
- `go vet` for dependency-free Phase 7 packages — PASS.

Phase 7 remains **PARTIAL** because migration `00008`, the pgvector HNSW index and the PostgreSQL retriever cannot be executed against a real PostgreSQL/pgvector instance in this sandbox. No retrieval-isolation PASS is claimed until that integration test is run externally.


## Phase 8 implementation

- `POST /api/v1/stories` creates a Story owned by the configured local single-user actor;
- Story Setup generates the six authoritative component keys: `story_bible`, `player`, `world`, `initial_cast`, `visual_bible`, `opening_situation`;
- locked components are excluded from regeneration;
- manual component edits increment revision and preserve lock state;
- setup generation is Story-scoped and can run before any Timeline exists;
- setup generation provenance references a Story-scoped GenerationJob;
- Story start requires every setup component to be `ready`;
- Story start transaction creates Main Timeline, Chapter 1, first Scene, first committed Beat, initial choices events and marks the Story started;
- `GET /api/v1/timelines/{timelineId}/current` exposes the first Reader state;
- server bootstrap now wires Story Setup and the gameplay generation runner;
- responsive frontend implements New Story → Setup Review → Start → Reader and allows opening choices to submit gameplay actions through SSE.

### Corrective architecture work discovered while entering Phase 8

Two earlier implementation deviations were corrected without rewriting old migration history:

1. `generation_jobs.timeline_id` is now nullable, matching the authoritative rule that Story Setup exists before Main Timeline.
2. v1 embedding baseline is corrected from the accidental `vector(1024)` implementation to `vector(384)` for `deepvk/USER2-small`.

The EmbeddingProvider port was also aligned with ADR-EMBED-001 (`EmbedQueries`, `EmbedDocuments`, capabilities), and a local sentence-transformers CPU adapter/provider process for `deepvk/USER2-small` was added.

### Phase 8 deterministic tests executed

- full setup generation creates all required components — PASS;
- locked component survives targeted regeneration — PASS;
- unlocked component revision increments — PASS;
- manual edit changes source/revision and can lock component — PASS;
- incomplete setup cannot start — PASS;
- complete setup produces Main Timeline + opening Beat + four choices — PASS;
- USER2-small adapter contract requires 384 dimensions — PASS;
- Context Builder and gameplay regression packages — PASS;
- `go vet` for dependency-free Phase 8 packages — PASS;
- Python embedding-provider source syntax — PASS.

### Phase 8 remaining acceptance work

The authoritative exit criterion is a real UI flow reaching the first playable Beat. The implementation path exists, but this sandbox cannot run the required complete browser/API/PostgreSQL/real-LLM environment because `pgx`/`chi` dependencies and PostgreSQL are unavailable. Frontend dependencies are also absent, so Vitest/TypeScript cannot be fully executed here. Phase 8 therefore remains PARTIAL rather than receiving a false PASS.


## Phase 9 implementation

- full-screen `/director/:timelineId` mobile/desktop route with explicit `← Назад к истории`;
- Reader stores Beat anchor + scroll offset before entering Director and restores it after return;
- typed Director read model covers Characters, directed Relationships, Stats, Inventory, Facts, Knowledge, Beliefs, Threads and active instructions;
- typed exact commands include stat edits, item transfer/location, Fact, Knowledge, Belief, Character State, Relationship and Thread changes;
- no raw database JSON editor is required for normal Director controls;
- every exact Director edit creates a safety SavePoint before Canon mutation;
- exact edit uses Timeline `semantic_revision` stale-write protection;
- exact projection update, semantic `director_exact_edit_applied` Story Event, Timeline head advance, semantic revision advance and audit log are committed in one PostgreSQL transaction;
- `director_audit_log` stores before/after state without conflating audit records with Story events;
- soft/high/hard Director instructions remain separate from exact state mutations;
- active instructions are injected into the next Director role input, including hard instructions;
- hard instruction does not directly mutate Facts/Stats/Inventory.

### Phase 9 API

- `GET /api/v1/timelines/{timelineId}/director`
- `POST /api/v1/timelines/{timelineId}/director/exact`
- `POST /api/v1/timelines/{timelineId}/director/instructions`
- `GET /api/v1/timelines/{timelineId}/director/history`

### Phase 9 deterministic tests executed

- exact edit creates safety SavePoint before repository apply — PASS;
- hard instruction remains distinct from exact state edit — PASS;
- active hard instruction reaches the next Director role context — PASS;
- Director empty-state API/browser rendering and global/minor objective projection — PASS;
- generation/domain regression suite — PASS;
- `go vet` for dependency-free Director packages — PASS.

### Phase 9 remaining acceptance work

Gate 7 cannot honestly be marked PASS in this sandbox because PostgreSQL/pgx and browser dependencies cannot run here. The real integration gate still must prove exact stat persistence, item transfer ownership constraints, Fact/Knowledge/Belief separation, semantic event/audit atomicity and mobile anchor restoration in an actual browser.

Self-review also identified an inherited replay limitation: the current generic `projection.Apply` tracks event sequence but does not yet reconstruct every narrative projection payload from semantic events. Director exact edits therefore append the correct semantic event and update SQL projections atomically, but event-only reconstruction of the changed narrative state without a later snapshot is not yet proven. This must be addressed in the reliability/replay work before release and prevents a false full PASS.


## Phase 10 implementation

- Reader now exposes `storyId` and links directly to the Story Save/Timeline library;
- Save Library aggregates all non-deleted Timelines and their SavePoints without flattening branch ancestry;
- Save cards expose name, note, kind, pin, event/Beat position, optional display slot and independent thumbnail lifecycle metadata;
- manual display slots are unique per Timeline at DB level;
- thumbnail status/asset are presentation/operational metadata and are never required for SavePoint validity;
- current SavePoint snapshot/FK integrity remains unchanged;
- safe load UX uses `preview` followed by explicit named Fork; destructive in-place restore remains blocked by the existing MVP policy;
- new fork retains `parent_timeline_id` and `forked_from_save_id`;
- phone-first UI presents Timelines, saves, pin controls, preview, branch naming and a bottom-sheet confirmation;
- opening a branch navigates Reader to the branch Timeline; source and sibling branches remain untouched.

### Phase 10 API

- `GET /api/v1/stories/{storyId}/timelines`
- `GET /api/v1/stories/{storyId}/save-library`
- `GET /api/v1/timelines/{timelineId}/saves`
- `POST /api/v1/timelines/{timelineId}/saves`
- `PATCH /api/v1/saves/{saveId}`
- `POST /api/v1/timelines/{timelineId}/fork`
- `POST /api/v1/saves/{saveId}/restore` with `preview` or safe `fork`

### Phase 10 deterministic tests executed

- existing save/fork/restart rebuild regression — PASS;
- Save Library preserves multiple branches — PASS;
- save presentation slot/pin metadata survives listing — PASS;
- invalid display slot rejected — PASS;
- Fork does not mutate source or sibling Timeline — PASS;
- save/domain/Director/generation regression packages — PASS;
- targeted `go vet` — PASS.

### Phase 10 remaining acceptance work

The complete Gate cannot be marked PASS in this sandbox. PostgreSQL/pgx/chi dependencies are still unavailable for full integration execution and frontend dependencies are absent, so the real mobile browser flow cannot be run here. Required external verification: migrations through `00012`, save slot uniqueness, snapshot FK integrity, safe fork from old SavePoint, sibling isolation, browser back/navigation and small portrait viewport without horizontal overflow.

Thumbnail generation is deliberately not coupled to save correctness: a missing/failed thumbnail renders a placeholder and must never invalidate a SavePoint.


## Phase 11 implementation

- provider-neutral asynchronous image application service uses the existing `ImageProvider` port;
- image assets have an independent lifecycle: `queued → running → ready|failed`;
- Beat/Story Canon is never rolled back when image generation fails;
- image provenance pins provider/model/profile/config revision at asset creation;
- later provider switching cannot rewrite historical image provenance;
- migration `00013_image_generation.sql` adds `image_assets` plus DB trigger protection for immutable provenance;
- image assets can be associated with Beat, Save thumbnail, Story cover or Character purpose;
- artifact references are nullable until successful completion;
- Reader renders ready image or a stable responsive queued/failed placeholder without blocking text/choices;
- FakeImageProvider contract is exercised for success and provider failure.

### Phase 11 deterministic tests executed

- provider failure after queue leaves image asset failed without failing the queue/Story path — PASS;
- provider/model change after queue does not rewrite asset provenance — PASS;
- successful FakeImageProvider completion records artifact reference — PASS;
- save/generation/domain regression packages — PASS;
- targeted `go vet` — PASS.

### Phase 11 remaining acceptance work

The image phase cannot be marked PASS in this sandbox: `00013` cannot be exercised on real PostgreSQL here, no documented real image runtime/workflow is available in this execution environment, and browser dependencies remain absent. The real gate must verify image job persistence/restart, retry behavior, actual artifact storage/serving, SSE `image ready` delivery, historical image retention across provider changes, and phone rendering.

The current async runner is deliberately small and process-local. It proves the architecture boundary and failure isolation but is not claimed as the final durable jobs/recovery subsystem; that remains part of reliability hardening.


## Phase 12 implementation

- migration `00014_ai_provider_settings.sql` adds a single active-config pointer while keeping every `ai_config_revisions` row immutable;
- activating a revision changes only configuration used for future work;
- provider/model/profile public settings are versioned; old revisions are never rewritten;
- API exposes only `storyLlmSecretConfigured: true|false`, never the secret value;
- Story LLM credentials are stored through a secret-store port and keyed by config revision, preventing a new credential from silently changing an old revision;
- initial environment API key is associated with the boot active revision only;
- Story Setup captures the active StoryLLM once per setup generation;
- gameplay Runner resolves/captures a provider pipeline at submission time, before its goroutine begins;
- duplicate idempotency submission reuses its existing generation rather than resolving a new provider;
- switching active revision after a generation has begun cannot change that generation's captured provider client;
- mobile Provider Settings UX creates a new revision, activates an existing revision, displays model/provider history and treats API key as write-only password input.

### Phase 12 API

- `GET /api/v1/settings/ai`
- `GET /api/v1/settings/ai/active`
- `POST /api/v1/settings/ai/revisions`
- `POST /api/v1/settings/ai/revisions/{revisionId}/activate`

`GET` responses contain no provider secret values.

### Phase 12 deterministic tests executed

- serialized safe settings view does not contain configured API key — PASS;
- switching settings creates a new immutable revision — PASS;
- old revision provider/model remains unchanged — PASS;
- credentials are scoped per revision — PASS;
- reactivating an old revision retains its old credential association — PASS;
- Runner resolves provider once for each new submission — PASS;
- duplicate submission does not resolve a new provider — PASS;
- Story Setup / generation / domain regressions — PASS;
- targeted `go vet` — PASS.

### Phase 12 remaining acceptance work

Phase 12 remains PARTIAL because the real PostgreSQL migration/trigger/API path and frontend browser path cannot run in this sandbox. The in-memory secret store intentionally does not claim durable encrypted secret persistence: environment provisioning remains the restart-safe baseline until a documented encrypted secret-storage mechanism is available. No plaintext provider secret is added to Story JSON, `ai_config_revisions`, logging or GET APIs.

Image and embedding settings are versioned in the revision model, but their real adapters/jobs must also be wired through the same active-revision selection during later durable-jobs/reliability hardening before the provider-switching gate can be considered globally complete.


## Phase 13 implementation

- migration `00015_generation_jobs_reliability.sql` extends `generation_jobs` with durable request payload, attempt policy, availability/backoff, lease owner/expiry, heartbeat, cancellation request and last error;
- `(timeline_id, story_id)` composite FK prevents generation metadata from crossing Story boundaries;
- non-empty `(timeline_id, request_id)` is unique, providing restart-safe submission idempotency;
- browser-supplied Story ID is not trusted: Story ownership is resolved server-side from the target Timeline;
- HTTP action submission now persists a durable GenerationJob instead of directly starting a generation goroutine;
- PostgreSQL claiming uses `FOR UPDATE SKIP LOCKED` so concurrent workers cannot claim the same queued row;
- worker captures immutable config revision/provider provenance already stored on the job;
- original `PlayerAction` is persisted as JSON and can be reconstructed after backend/worker restart;
- expired running leases are requeued (or failed/cancelled according to attempt/cancel policy) during worker startup recovery;
- heartbeat extends only the owning live lease; lease loss cancels local execution;
- explicit cancellation is persisted and exposed through `POST /api/v1/generations/{generationId}/cancel`;
- bounded execution timeout routes the job through retry/failure policy rather than declaring success;
- backend shutdown/context cancellation is not treated as completed work;
- exponential bounded retry backoff is deterministic at the application layer;
- worker finalization uses a short independent context so successful provider work can be persisted even if the request context has ended;
- server bootstrap now uses the durable runner + PostgreSQL worker rather than the old process-local gameplay runner.

### Phase 13 deterministic tests executed

- lease claim/heartbeat/foreign-owner rejection — PASS;
- max-attempt enforcement — PASS;
- cancellation state policy — PASS;
- successful worker completion — PASS;
- provider/executor failure routes to retry policy — PASS;
- explicit cancellation terminates execution — PASS;
- execution timeout routes to retry/failure, not completion — PASS;
- durable submission persists Story, config revision and original action payload — PASS;
- duplicate request key returns the same durable Generation ID — PASS;
- generation/config/domain regression packages — PASS;
- targeted `go vet` — PASS.

### Phase 13 remaining acceptance work

Phase 13 remains PARTIAL because PostgreSQL is unavailable in this sandbox. The real reliability gate must run at least two worker processes against the same database and verify `SKIP LOCKED` exclusivity, lease expiry after a killed worker, restart recovery, racing duplicate submissions, cancellation during provider work, max-attempt terminal failure, and stale-head Canon protection after a retried generation.

SSE delivery is still process-local: the durable GenerationJob survives a restart, but an SSE client connected to a dead backend obviously does not. Reconnect/status replay from persisted generation state should be added during final frontend/reliability hardening.

The known narrative semantic replay gap remains open and is the next high-priority correctness item: durable execution is not sufficient for release until Story Events + snapshots can reconstruct every implemented narrative mutation.


## Phase 14 implementation

- `projection.Apply` no longer advances only event sequence: it applies a strict typed semantic reducer;
- unknown semantic event types fail replay with `ErrUnsupportedSemanticEvent` rather than being silently treated as reconstructed;
- Story opening now emits reconstructable `chapter_created`, `scene_started`, `beat_committed`, and `choices_ready` events;
- gameplay `beat_committed` now contains Beat ID, Scene ID, position, kind, text and status;
- a `GenerationTarget` port resolves the authoritative active Scene and next Beat position before gameplay commit;
- server runtime wires the PostgreSQL GenerationTarget into every generation pipeline;
- Canon `AppendSemantic` exports current narrative projection, applies the exact same reducer to the proposed event batch, materializes the resulting projection, appends Story Events and advances Timeline head in one Unit-of-Work transaction;
- PostgreSQL narrative materializer was converted from insert-only fork behavior to idempotent upserts so it can safely serve live and replay projection updates;
- gameplay head commits now also advance Timeline semantic revision so stale Director forms cannot overwrite a newer gameplay state;
- Director exact-edit events replay typed mutations for Stats, Item ownership/location, Facts, Knowledge, Beliefs, Character State, Relationship and Story Thread changes;
- Fact / Knowledge / Belief remain separate during replay;
- live Canon materialization and restart `Rebuild` are tested against the same Beat result.

### Phase 14 deterministic tests executed

- opening Chapter/Scene/Beat reconstruction — PASS;
- subsequent gameplay Beat reconstruction — PASS;
- Director stat replay — PASS;
- Fact replay — PASS;
- Knowledge replay against canonical Fact — PASS;
- Item transfer replay — PASS;
- unknown semantic event rejection — PASS;
- live Canon materialization equals restart replay for a Beat — PASS;
- contiguous event-sequence enforcement — PASS;
- save/jobs/generation/domain regression suites — PASS;
- targeted `go vet` — PASS.

### Phase 14 remaining acceptance work

Phase 14 remains PARTIAL because the real PostgreSQL projection/materializer path cannot be executed in this sandbox. The external gate must prove, against migrations through `00015`, that a long mixed sequence of gameplay + Director events produces byte/semantic-equivalent state after: live execution, snapshot creation, backend restart, replay from snapshot, and full replay from origin where supported.

The reducer is intentionally strict. Future semantic event types must add a reducer before they can be committed as Canon. Director instruction lifecycle is still stored in its dedicated projection and snapshot, but instruction create/complete/expire is not yet represented as Story Event reducers; that remains an explicit release-hardening item rather than being silently claimed as replay-complete.


## Phase 15 implementation

- generation updates are persisted in `generation_updates` with monotonic per-generation sequence;
- normalized SSE events now carry `sequence`;
- persisted update is written before live hub publish, so the live client observes the same sequence stored for replay;
- SSE endpoint accepts both explicit `?after=N` and browser-standard `Last-Event-ID`;
- reconnect replays only updates after the last acknowledged sequence and filters boundary duplicates;
- migration `00016_generation_updates.sql` adds durable normalized generation-update history;
- one explicit user-facing image generation endpoint is now the temporary image integration boundary: `POST /api/v1/images/generate`;
- no automatic image generation is attached to Beat commits at this stage;
- `GET /api/v1/images/{imageId}` is available only to read queued/running/ready/failed state of the asset created by that generation request;
- PostgreSQL `ImageRepository` persists the existing ImageAsset lifecycle/provenance;
- a simple HTTP ImageProvider adapter calls exactly one configured external endpoint with `{prompt,width,height}` and expects `{artifactRef}`;
- provider/model/profile/config revision are still pinned before the request, so this simplification does not weaken provenance/provider switching rules;
- if the active image provider is `fake`, the existing deterministic FakeImageProvider remains usable for development;
- if a non-fake provider is active but `imageEndpoint` is not configured, generation fails explicitly rather than silently falling back;
- frontend API helpers expose the single image generation call and image status lookup, but no automatic Beat-image workflow is introduced.

### Phase 15 deterministic tests executed

- persisted sink assigns sequence before live publish — PASS;
- cursor replay returns only updates after the requested sequence — PASS;
- Fake image provider failure remains isolated from Story Canon — PASS;
- image provider provenance remains pinned after provider switch — PASS;
- single external image endpoint contract — PASS;
- generation/jobs/replay/domain regression suites — PASS;
- targeted `go vet` — PASS.

### Phase 15 remaining acceptance work

Phase 15 remains PARTIAL because PostgreSQL/chi/frontend runtime dependencies are unavailable in this sandbox. The external gate must verify migration `00016`, Last-Event-ID reconnect through a real browser, restart between generation updates, image asset persistence, and an actual configured image endpoint.

The temporary image approach is intentionally simple: one explicit endpoint request creates one ImageAsset and starts one asynchronous provider call. Automatic Beat images, durable image-job retries/restart recovery, prompt composition from Story context, and image-ready SSE are deferred until explicitly needed.


## Phase 16 — Perchance browser image generation

The temporary one-endpoint image provider from Phase 15 is no longer the product path. It has been replaced by the specified browser-worker architecture.

### Implemented

- `image_generations` durable PostgreSQL queue tied to Story + real Scene + optional source Beat;
- provider/config/prompt/style/resolution/count provenance is immutable;
- default provider `perchance_browser`, model `text-to-image-plugin`;
- Digital Painting provider preset is frozen per generation;
- fixed 768×768 resolution and exactly two variants;
- one browser job at a time; two `generateImage()` calls execute in parallel inside Perchance;
- statuses: pending/running/done/failed;
- five-minute leases; max three attempts; bounded 5s/20s retry backoff;
- expired leases are reclaimable;
- worker heartbeat/status separate from backend health;
- public Scene image API and internal worker namespace;
- exact two-image validation;
- 40 MiB worker result request limit;
- JPEG/PNG/WebP MIME and binary-signature validation;
- no base64 in PostgreSQL;
- `ImageStorage` port + atomic local filesystem implementation;
- deterministic file paths and idempotent worker result behavior;
- `scene_images` variants 1/2 and selected Scene image;
- Regenerate creates a new ImageGeneration and preserves previous generations;
- automatic scheduling after initial Story Setup Scene commit;
- post-Canon `scene_started` observer handles future semantic Scene creation;
- image scheduling/generation failure never rolls back Story Canon;
- structured lifecycle logs omit image base64;
- backend binds to loopback by default;
- Violentmonkey script is dumb transport only;
- Perchance DOM mailbox runtime owns `generateImage()` calls;
- no CAPTCHA/Turnstile bypass;
- Reader polls image status, shows two skeletons, two variants, selection, failure/retry and regeneration;
- generated media is served from `/media`.

### Additional launch hardening completed

- Story Setup synchronous generation jobs were corrected to satisfy the later durable lease constraint;
- Story Setup now pins `ai_active_config`, not merely the numerically latest revision;
- real OpenAI-compatible StoryLLM now actually loads the versioned role prompt files from `backend/prompts/v1`;
- Docker Compose now includes an explicit migration service;
- host backend defaults to `127.0.0.1:8787`;
- Vite proxies `/api`, `/internal`, `/health` and `/media`;
- local launch and Perchance worker documentation added.

### Phase 16 deterministic checks

- ImageGeneration defaults/provenance — PASS;
- exactly two results — PASS;
- idempotent repeated result — PASS;
- MIME spoof rejection — PASS;
- lease/retry domain policy — PASS;
- local ImageStorage path safety — PASS;
- Story/Canon/generation/save/Director regression suites — PASS;
- versioned StoryLLM prompt contracts — PASS;
- Violentmonkey syntax — PASS;
- Perchance runtime worker syntax — PASS;
- Docker Compose YAML parse — PASS;
- targeted `go vet` — PASS.

### Status

**PARTIAL / ready for local MVP launch attempt.**

The project is not marked release PASS because real llama.cpp generation, a complete backend/Violentmonkey/Perchance image round trip, mobile browser E2E, and multi-worker restart/load gates have not been executed. See `PROJECT_READINESS.md` and `RUN_LOCAL.md`.


### Final sandbox verification for Phase 16

Executed successfully:
- targeted Go tests for config, prompt catalog/StoryLLM adapter, image generation/storage, Story Setup, Canon/replay, gameplay generation, durable jobs, Saves and Director;
- targeted `go vet`;
- Violentmonkey JavaScript syntax check;
- Perchance runtime worker JavaScript syntax check;
- Docker Compose YAML parse.

The historical dependency restrictions no longer apply to the current workspace: `backend/go.sum`, Go modules, and frontend dependencies are present and the complete suites pass. The remaining gaps are runtime/E2E gates listed in the current verification snapshot above.

## Remaining environment/runtime limitations preventing release PASS

- no selected GGUF/llama.cpp story generation was exercised in this audit;
- no real image job was submitted through Violentmonkey and returned from Perchance;
- no complete mobile browser acceptance pass was run;
- no multi-worker kill/restart/load test was run.

These are verification blockers. They are not being converted into false PASS results.

## Architecture deviations / decisions

- No new architecture stack was introduced.
- Restore follows the current authoritative MVP policy: safe fork is the default; destructive restore is not implemented as a hidden head rewrite.
- Earlier Phase 1 snapshot projection was expanded in Phase 3 to schema version 2 because exact historical branching requires the implemented narrative projection, not only event counters.
- Fork remaps Timeline-local UUIDs because current PostgreSQL DDL uses globally unique UUID primary keys for Chapter/Scene/Beat and other branch-local rows.
- No LLM/Image/Embedding provider implementation has been introduced yet.

## Known issues

- Full pgx/chi compile, fresh migrations, and PostgreSQL integration are verified; browser/LLM/image-provider E2E remains open.
- Advanced destructive restore (safety save + explicit active-lineage transition/audit) is intentionally not enabled in the MVP HTTP path; safe fork is implemented and preferred by current architecture decisions.
- Frontend Timeline/Save browser UX belongs to later UI phases; Phase 3 currently provides backend service/API foundation.

## Next

1. Execute the first-launch checklist on the target machine.
2. Fix any real PostgreSQL/migration/browser/Perchance integration defects found there before adding more product scope.
3. Wire ContextBuilder + live memory retrieval into the Story generation pipeline for long-running story coherence.
4. Complete remaining replay/observability/security/release gates before production PASS.
