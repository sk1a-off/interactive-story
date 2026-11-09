# Full-pipeline latency baseline — method and instrumentation

Stage 3 starts with measurement of the current implementation. This increment
adds a full-turn mode to `cmd/llmbench` and a read-only runtime metrics endpoint.
It does not change model routing, prompts, generation profiles, or production
settings.

## Why a separate mode

The original `llmbench` runs independent role contracts. `hybridbench` runs the
pipeline but has historical routing/comparison options and no paired memory
exposure. Neither should be reported as the requested baseline unchanged.
`llmbench -turn-bench` runs the production `generation.Pipeline` with frozen
synthetic targets and controlled memory-off/on pairs.

The configured mode pins the active AI revision and Prompt Studio revision once,
reads credentials through the existing read-only store, and opens the metadata
database with `default_transaction_read_only=on`. It does not read user stories,
timelines, journal content, or Reader choices. Prompt and fixture hashes are
recorded; credential values and raw provider errors are excluded.

The measured configuration uses the stored Safe Hybrid routing and explicit
launcher switches: turn planner, provisional preview, fast state evaluation, and
journal-evidence isolation. They are benchmark arguments, not changes to `.env`
or active settings. The selected current chain is:

```mermaid
flowchart LR
  A[Player action and frozen target] --> M[Optional bounded memory]
  M --> P[Turn planner]
  P --> W[Writer]
  W --> V[Existing draft-ready preview]
  W --> E[World evaluator]
  W --> S[State evaluator]
  W --> Q[Quest evaluator]
  S --> C[Choices]
  Q --> C
  E --> G[Go validation]
  C --> G
  G --> K[In-memory Canon capture]
  K --> R[Completed event]
```

Choices wait for state and quest changes, and can overlap world evaluation.
Planner merging, the parallel evaluator/choices path, and provisional preview
already exist in this checkout. They are not counted as new optimizations.

## Experimental unit and timing

There are three hand-authored scenarios: conversation, inspection, and danger.
Two paired repeats produce six pairs / twelve turns. Each pair uses the same
initial target, action, prompt revision, model routing, and flags; the treatment
adds three older synthetic memories through real PostgreSQL retrieval and
Context Builder at k=8 / 1024 estimated tokens. Memory queries use live USER2 in
the configured run. Actual injected evidence counts are recorded; skipped memory
does not qualify as an effective paired exposure.

Scenario order is seeded and shuffled. Off/on order reverses between repeats
within each scenario. The seed controls ordering, not nondeterministic provider
generation. Generated prose and usage can differ between arms. Paired deltas
record time, words, and tokens so a latency difference cannot be silently treated
as pure lookup overhead.

The benchmark records:

- writer completion, first available preview/completed event, and completed turn;
- full pipeline stage metrics and each role's start/end/duration;
- reported input/output tokens, repeated role calls, structured repairs, and
  model escalation provenance;
- physical HTTP POST/GET counts, statuses, and transport failures. Managed-agent
  polling is not conflated with retries or logical role invocations;
- paragraphs, word count, choices, actual evidence exposure, and success/failure.

Timing uses sub-millisecond precision for fast fake runs. Distributions include
succeeded turns; failures remain explicit rows/counts. Parallel role durations
overlap and must not be summed into wall time. Usage is as returned by the
logical client and may omit a rejected/escalated request; zero does not prove
zero provider consumption.

This is a pipeline baseline, not browser end-to-end timing. It excludes durable
queue wait, per-job configuration resolution, telemetry persistence, real SQL
Canon commit, browser networking/rendering, and illustration jobs. A validated
event capture simulates the final commit. Literary quality is not approved by
pipeline success. Raw synthetic prose is saved for review.

## Fixture safety and interruption

Model document embeddings are prepared before inserting memory fixtures. There
is no open fixture transaction during remote LLM calls. New fixture owners,
stories, root timelines, and derived vectors are committed only in the explicitly
selected isolated database. No story events or snapshots are committed.

Cleanup removes only stories with the newly generated owner and the benchmark
title, deletes timelines before stories according to the existing foreign keys,
and verifies removal of the fixture owner. Results record the owner ID and cleanup
outcome. SIGINT/SIGTERM cancel requests and allow cleanup/checkpoint saving. A
forced process kill still requires checking the recorded owner in the isolated
database. Reports checkpoint after each completed turn; a running/failed partial
report must not be presented as a finished baseline.

## Runtime diagnostics

`GET /api/v1/generations/{generationId}/metrics` reads recorded metrics for the
configured local owner. It returns job status, attempt count, attempt timings,
and grouped role/model call counts, latency, usage, repairs, recorded retries,
and failures. Missing/foreign/deleted-story jobs return the same 404. Invalid IDs
return 400; unavailable storage returns generic 503. Raw error text and request
payloads are not exposed.

The read uses a read-only REPEATABLE READ snapshot. Telemetry is best effort;
absent rows are not replaced with fabricated zero-time calls. `writerReadyMs`
means the writer finished, including when early preview is disabled. Stored retry
counts do not reconstruct physical HTTP traffic. No new migration is needed.

The executor logs a compact attempt timing summary; the observing model logs
per-role timing/usage without input/output bodies. Model latency is captured
before telemetry persistence, so recording time is not mislabeled as model time.

## Reproduction

From `backend`, with the existing local USER2 service running:

```bash
GOCACHE=/tmp/interactive-story-go-cache go run ./cmd/llmbench \
  -turn-bench -turn-provider configured \
  -turn-config-database-url 'postgres://story:story@127.0.0.1:5432/story?sslmode=disable' \
  -turn-fixture-database-url 'postgres://story:story@127.0.0.1:5432/story_image_settings_00036_check?sslmode=disable' \
  -turn-embedding-provider user2 -turn-repeats 2 \
  -turn-planner -turn-provisional -turn-fast-state -turn-journal-isolation \
  -output /tmp/full-turn-baseline.json
```

The configuration database is read-only; the fixture database must be isolated
and migrated through 00040. `-turn-secret-store` overrides the credential-store
path. `-turn-cases conversation` selects a pilot. For mechanical overhead/wiring,
use `-turn-provider fake -turn-embedding-provider fake` and omit the configuration
database. Fake results are marked `smoke_only`; they are not model-quality or
Gemini-speed evidence. Existing single-role `llmbench` mode remains available.
