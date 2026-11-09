# Stage 3.4 — reasoning effort experiment

## Decision

Keep the runtime defaults. The current Gemini service calls still request `low`,
Gemma keeps its previous `minimal` default, and the Antigravity writer/evaluators
are unchanged. No AI revision, prompt, model route, launcher switch or database
migration was changed. No new user-facing setting is introduced without an
established reason to enable it.

`minimal` is accepted by the current Gemini 3.5 Flash-Lite endpoint, but the
paired experiment does not establish a speed benefit. The main acceptance flag
is **false because of provider unavailability**, not because the 161 returned
answers failed the checked contracts. Preserve those failures when interpreting
this result; they are not zero-time successful generations.

## Capability boundaries

The [Google compatibility guide](https://ai.google.dev/gemini-api/docs/openai#thinking)
describes model-dependent reasoning levels. `minimal` does not mean guaranteed
zero thinking, and `none` is not interchangeable across model generations. A
separate two-request current-model pilot verified acceptance of both `low` and
`minimal` with identical frozen planning input before the main run.

The documented [Antigravity agent configuration](https://ai.google.dev/api/interactions-api#Resource:AntigravityAgentConfig)
exposes a backing model and total token budget, but no reasoning-depth field.
Changing `max_total_tokens` is not a thinking-level adjustment and may truncate
an agent run. The writer and world/quest evaluator paths therefore remain
unbenchmarked for this knob. Unsupported fields were not sent speculatively.
This leaves the dominant waits identified in the full-turn baseline unaffected.

The client now accepts explicit per-role experimental overrides only for the
known Flash-Lite model identities (`gemini-3.5-flash-lite` and
`gemini-3.1-flash-lite`) and `low`/`minimal` values. Model/provider/role/effort
validation rejects other combinations. The override map is copied into the
client snapshot. Production construction does not set it, so request bodies and
routing keep their previous defaults. Only Gemini 3.5 Flash-Lite was tested live.
Local llama.cpp and managed-agent overrides are rejected; their default paths
still work as before in protocol tests.

## Method

Eight fixed synthetic cases cover conversation/inspection/danger planning,
four distinct choices, one positive journal update, and three negative journal
cases (no new information, attempted acquisition, observation without possession).
Twelve paired repeats give 96 pairs / **192 logical requests**. Arm order reverses
per case on alternate repeats; case ordering is seeded, with two concurrent
paired workers. The main run lasted 100.19 s.

Inputs originate in the existing frozen pipeline fixtures, and both arms of each
pair have the same input SHA-256. AI/prompt revisions and credentials are pinned
through the existing read-only metadata path. Both arms use JSON-object output,
identical sampling/token limits and the current fast model. Schema mode is not
mixed into this experiment. No user-story content, SQL fixtures or Canon writes
are involved.

The probe calls the current fast route directly. It intentionally omits Safe
Hybrid quality escalation, so an unsupported knob or transient failure cannot
silently become a slower Antigravity result. This is a role experiment, not the
runtime retry policy or a full-turn comparison. Requests are bounded at 60 s.

Checks cover planner agency and transition validation, four distinct actions,
and production Pipeline validation/in-memory Canon replay for journal mutations.
Positive cases must commit relevant mutations; negative cases must commit none.
This is not a broad literary, continuity or branch-isolation acceptance corpus.

## Results and unavailable requests

| Mode | Logical requests | Validated replies | Provider failures | Validation failures |
| --- | --- | --- | --- | --- |
| `low` | 96 | 80 | 16 | 0 |
| `minimal` | 96 | 81 | 15 | 0 |

All 161 returned replies passed the stated checks. The 31 unsuccessful logical
requests ended with 30 HTTP 503 responses and one final quota exhaustion (429).
Existing credential rotation produced **246 physical POSTs** in total: 161 status
200, 30 status 503, 47 status 429 and 8 status 403. No GET polling, transport
failure, format fallback, structured repair or quality-model escalation occurred.
The 403/429 statuses are retained as numerical evidence; no credential values or
provider error bodies are reported. These observations do not diagnose which
Google project quota or credential caused each rejection.

Availability was similar in both modes. A fast-role provider failure can trigger
an expensive quality escalation in normal Safe Hybrid operation, but that cost
was deliberately not hidden in this direct-role comparison. Investigating quota,
backoff and request priority belongs to stage 3.7. Additional calls were not made
just to replace the failed observations with a cleaner-looking run.

The provider did not report `completion_tokens_details.reasoning_tokens` on any
main reply. Missing usage remains absent, not zero. Accepted request parameters
therefore do not prove the internal thinking budget actually used.

## Paired timing

Only **68/96** pairs have two successful validated replies; the other 28 pairs
remain explicitly excluded from paired-success timing. The source report's
per-arm latency distributions include failed calls as well. Successful reply
medians were 963.57 ms for `low` and 1012.29 ms for `minimal`; comparing those two
unpaired medians is not a causal effect estimate.

For complete successful pairs, median `minimal minus low` was **−39.24 ms**.
A hierarchical bootstrap that resamples the eight case clusters and then pairs
within each selected case (5,000 draws, seed 20261109) gave a descriptive 95%
interval of **−112.87 to +141.85 ms**, crossing zero. This interval is conditional
on provider availability and this small fixed scenario corpus; outages may bias
which pairs qualify. It is not a production-wide confidence guarantee.

| Case | Low passed | Minimal passed | Complete pairs | Median paired delta, ms |
| --- | --- | --- | --- | --- |
| `choices` | 6/12 | 10/12 | 4 | +125.9 |
| `planner/conversation` | 9/12 | 8/12 | 6 | +139.1 |
| `planner/danger` | 11/12 | 11/12 | 10 | -66.0 |
| `planner/inspection` | 8/12 | 7/12 | 5 | +7.4 |
| `state/intention` | 12/12 | 12/12 | 12 | -28.5 |
| `state/observation` | 12/12 | 12/12 | 12 | -150.5 |
| `state_evaluator/negative` | 12/12 | 12/12 | 12 | -35.8 |
| `state_evaluator/positive` | 10/12 | 9/12 | 7 | +336.5 |

The case-level deltas go in both directions. The test gives no basis to change
working defaults or claim a full-turn speed improvement. In particular, no writer
speed or prose-quality result is inferred from service-role timings.

Raw artifacts:
[192-call comparison](benchmarks/reasoning-role-comparison-2026-11-09.json),
[derived paired analysis](benchmarks/reasoning-analysis-2026-11-09.json),
[capability pilot](benchmarks/reasoning-capability-pilot-2026-11-09.json),
[fake mechanical smoke](benchmarks/reasoning-role-fake-2026-11-09.json).
The derived analysis records the source-file SHA-256; original reports are copied
byte-for-byte. The fake smoke uses 32 calls and is labeled `smoke_only`.

## Diagnostics and reproduction

`StoryResponse` and offline call traces distinguish requested/effective wire
reasoning effort and optional reasoning-token usage. Role logs include both.
No new metric columns are required. Optional malformed, null or negative usage
is ignored rather than making otherwise valid story output unusable.

From `backend`:

```bash
go run ./cmd/llmbench -reasoning-bench -turn-provider configured \
  -turn-config-database-url 'postgres://story:story@127.0.0.1:5432/story?sslmode=disable' \
  -turn-fast-state -turn-repeats 12 -output /tmp/reasoning-role-comparison.json
```

`-turn-cases planner/conversation` selects the pilot; `-turn-repeats 1` makes one
pair. Use `-turn-provider fake` without a database for mechanical checks. The
`-turn-fast-state` flag mirrors the current selected launcher path; omit it to
exclude all state-evaluator cases. It does not change the application switch.
Reports checkpoint after each request; interrupted/unsupported runs are labeled
explicitly. An HTTP 400/422 stops the experiment, with no hidden reasoning-level
fallback. A completed observational run can have `acceptancePassed=false`.

## Validation and remaining limits

New tests cover per-role opt-in, immutable override snapshots, unchanged writer
and local/agent defaults, rejected model/value combinations, bounded format
fallback preserving the effort treatment, no fallback on unsupported effort,
reported zero versus missing/malformed usage, frozen-case scope, planner agency,
journal replay, exact paired inputs and summaries retaining failures.

Before and after changes, Go formatting/vet/all tests and the full race suite
with isolated PostgreSQL passed; frontend dependency install, typecheck, lint,
all 48 tests and production build passed. The dependency audit still reports
the same 14 pre-existing findings. The earlier 24-output live schema replay
also passes with the shared replay helper. These engineering checks do not turn
the 31 unavailable experiment requests into successful calls.

No live app/browser or local model was exercised, and no candidate was deployed.
The next planned point is prefix-cache measurement. The current evidence also
makes provider availability and the later rate-limit work material priorities.
