# Stage 3.3 — optional response schemas

## Decision and scope

Response schemas are implemented as an opt-in immutable AI-revision setting,
`storyLlmJsonSchema` (default false). The Russian AI Settings control is
“Стабильный формат ответов”; save a new revision to enable/disable it. Existing
revisions stay off, and no active setting, prompt or model route was changed.
The experiment does not justify enabling schemas to improve speed: both arms
already passed all shape/evidence checks, while several schema calls took longer.

All nine roles in the ordinary turn chain have shapes: action interpreter,
director, pacing, turn planner, writer, world/state/quest evaluators, and choices.
`structured_repair` selects the schema of its original role when supported.
There are 21 registered Prompt Studio roles, rather than the old plan's 20.
Flexible setup/editor/image/extractor contracts retain their existing JSON-object
format. The removed lore guard is not restored. This is not a claim of schema
coverage for every editor or setup operation.

Schemas constrain field types, required core fields and exactly four choices.
They preserve optional mutation fields and do not replace evidence/UUID/agency,
journal balance, objective lifecycle or expected-head validation. The envelope
uses `strict: false` to preserve optional fields rather than forcing fabricated
values into every mutation. There are no new dependencies or migrations.

## Provider protocols and bounded fallback

- Chat completions use `response_format.type=json_schema` and a role envelope.
- Antigravity Interactions use `response_format.type=text`, JSON MIME type and
  `response_format.schema`.
- Default-off and roles without schemas retain their original wire format.
- Only an explicit schema/response-format rejection with status 400 or 422
  permits one reissue using the original JSON-object format. The existing
  embedded Google error envelope is recognized too.
- Authentication/quota/transport/server failures, malformed responses and
  ordinary invalid schemas do not trigger format fallback. Existing credential
  rotation and Safe Hybrid escalation stay responsible for their own errors.
- Rejection is remembered in an atomic flag for that client instance, so its
  later calls avoid repeated schema negotiation. Concurrent already-started
  schema requests can each negotiate once. A newly resolved job/client may probe
  again; there is no permanent global provider blacklist.
- Logs include effective format and schema fallback. The known extra protocol
  request contributes one recorded retry to the existing metrics API, separately
  from `structured_repair`. Usage/retry telemetry remains best effort and cannot
  reconstruct every hidden or escalated request.

The keyword subset was checked against Google's documentation and exercised on
real replies: [structured outputs](https://ai.google.dev/gemini-api/docs/structured-output),
[OpenAI compatibility](https://ai.google.dev/gemini-api/docs/openai),
[Interactions text response format](https://ai.google.dev/api/interactions-api#Resource:ResponseFormat).
Unsupported string/count/uniqueness constraints are not used. Application code
still checks non-empty text, distinct choices and semantic validity.

## Live role experiment

The read-only active configuration and prompt revision were pinned once. Nine
frozen cases × two paired repeats × two formats produced 36 logical calls from
18 pairs. Two workers ran independent pairs; format order reversed between
repeats for each case. Inputs are captured from the production pipeline with a
scripted writer, then positive/negative evaluator evidence is frozen explicitly.
Every pair has identical input hashes; provider nondeterminism remains.

- 36/36 logical calls passed the stated checks; all 18 schema-arm responses
  report effective `json_schema`, with no format fallback.
- 37 physical POSTs: 36 status 200 and one status 503. No GET polling or transport
  failure was observed. One schema state-positive call hit a fast-model 503 and
  succeeded through existing Antigravity escalation, inflating that arm's latency.
- There were no `structured_repair` calls. The object arm also had no shape
  failures, so no repair-rate improvement was demonstrated.
- Checks include required plan/text fields, four distinct choices, non-empty
  positive mutations, empty negative mutations and verbatim new-beat quotes.
- All 24 live evaluator outputs were replayed separately through production
  Pipeline validation and in-memory Canon capture: positive cases produced
  relevant mutations and negative cases produced none, in both formats.

Per-case median seconds over **two** successful observations; the table is a
compatibility/screening result, not a reliable production latency estimate.

| Frozen case | JSON object | JSON schema |
| --- | --- | --- |
| `choices` | 1.30 | 1.34 |
| `quest_evaluator/negative` | 10.91 | 11.65 |
| `quest_evaluator/positive` | 32.21 | 71.57 |
| `state_evaluator/negative` | 1.23 | 0.87 |
| `state_evaluator/positive` | 0.94 | 28.30 |
| `turn_planner` | 1.43 | 1.51 |
| `world_evaluator/negative` | 14.01 | 14.38 |
| `world_evaluator/positive` | 48.14 | 40.78 |
| `writer` | 22.16 | 33.42 |

The state-positive schema median mixes one Flash-Lite response and one escalated
Antigravity response. Writer medians are 22.16 s versus 33.42 s, and positive
quest medians 32.21 s versus 71.57 s. These small noisy samples do not isolate a
causal schema cost, but give no basis to promote the mode for speed. Reported
input/output usage is preserved; it may include provider-internal work and omit
rejected/escalated usage, so token totals are not a context-efficiency claim.

Raw evidence:
[role probe](benchmarks/schema-role-probe-2026-11-09.json),
[validator replay](benchmarks/schema-validator-replay-2026-11-09.json),
[fake smoke](benchmarks/schema-role-fake-2026-11-09.json),
[definition/input provenance](benchmarks/schema-probe-manifest-2026-11-09.json).
The live role probe started before fingerprint metadata was added; definitions
and timed payloads were unchanged, and its raw report is preserved verbatim.

The replay scripts all other roles and checks expected mutation counts; it is
not a full literary or branch-continuity review. Role latency does not measure a
whole turn, durable SQL commit, browser display or model streaming.

## Full-turn compatibility smoke

Two additional synthetic conversation turns ran all six roles through live
providers with schemas. Both succeeded with four choices each, all twelve calls
used `json_schema`, all twelve POSTs returned 200, and fixture cleanup was
verified in the isolated database. This uses the production pipeline with an
in-memory Canon capture, rather than a real durable SQL commit.

The memory-off turn exposed text at 66.06 s and completed at 107.17 s; the
memory-on turn exposed text at 28.51 s and completed at 80.27 s. Memory embeddings
were explicitly **fake** in this smoke: the on arm only verifies retrieval/wiring,
not USER2 semantic recall or overhead. There is no matching object-format arm in
this smoke and it is not a schema speed comparison. The schema fingerprint
matches the fake role report and the provenance manifest.

[Raw full-turn schema smoke](benchmarks/schema-turn-smoke-2026-11-09.json).

## Reproduction

From `backend`:

```bash
go run ./cmd/llmbench -schema-bench -turn-provider configured \
  -turn-config-database-url 'postgres://story:story@127.0.0.1:5432/story?sslmode=disable' \
  -turn-fast-state -turn-repeats 2 -output /tmp/schema-role-probe.json

SCHEMA_PROBE_REPORT=/tmp/schema-role-probe.json \
SCHEMA_REPLAY_OUTPUT=/tmp/schema-validator-replay.json \
  go test ./cmd/llmbench -run TestSchemaLiveReportThroughProductionValidators -count=1 -v
```

Use `-turn-provider fake` without the configuration database for a mechanical
smoke, explicitly labeled `smoke_only`. To check a full turn, the existing
`-turn-bench` also accepts `-turn-json-schema`; all other configuration, fixture
isolation and memory limitations from the baseline method still apply.

## Verification and rollout limits

Before and after checks passed: Go formatting, vet, all backend tests, the full
race suite with isolated PostgreSQL, frontend dependency install/typecheck/lint,
all 48 frontend tests, and production build. The baseline had 45 frontend tests;
three new setting/save/restore tests account for the increase. The dependency
audit still reports the same 14 pre-existing findings.

New tests cover both protocols, default off, unknown roles, dynamic repair,
explicit rejection, one bounded fallback, capability caching under concurrency,
embedded errors, failed fallback, preservation of ordinary failures, schema
keywords/map isolation, setting round-trip and actual UI save/restore.
No actual unsupported live endpoint was available; rejection fallback is tested
with controlled HTTP responses. Local llama.cpp protocol behavior is covered by
tests; a real local model and browser UI were not manually exercised.

The AI setting itself requires no migration. The overall current application
still expects migrations through 00040. This increment does not migrate or
restart the production database/application. Its configuration connection is
read-only; no credentials or user-story material are included in artifacts.

Next point: measure supported reasoning controls for service roles before
changing their defaults. Keep the schema option disabled unless a later result
provides a specific quality/reliability reason to enable it.
