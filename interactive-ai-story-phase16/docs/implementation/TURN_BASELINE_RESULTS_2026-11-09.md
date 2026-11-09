# Full-turn baseline — 2026-11-09

Stage 3.1 is measured; this increment establishes the comparison harness and
runtime diagnostics. No routing, prompt, profile, or production setting was
changed. The next increment should test a specific critical-path hypothesis
against this baseline before changing defaults.

## Scope and evidence

- Main run: 2026-11-09 18:11:15–18:26:39 UTC, twelve turns / six paired exposures.
- Frozen synthetic conversation, inspection, and danger scenarios, two repeats
  each; current stored Safe Hybrid configuration, live Gemini calls and USER2.
- Turn planner, provisional preview, fast state, and journal isolation enabled
  explicitly to match the selected launcher settings. These are existing paths.
- All twelve turns succeeded, with four choices each and six logical calls each.
  Every memory-on turn injected three documents; every off turn injected none.
- Seventy-two HTTP POSTs, all status 200; zero GETs, transport failures, repair
  calls, or deterministic choices/action-intent fallbacks in these observations.
- Synthetic fixtures were removed and cleanup verified. The production metadata
  connection was read-only. The temporary USER2 service was stopped afterward.
- A one-pair connectivity pilot is excluded from the main statistics.

Raw data:
[configured baseline](benchmarks/turn-configured-baseline-2026-11-09.json),
[fake mechanical smoke](benchmarks/turn-fake-smoke-2026-11-09.json).
[Method, limits, and reproduction](TURN_BASELINE_METHOD_2026-11-09.md).
The main run started before a subsequent embedding-health preflight and report
note were added; neither changes the timed pipeline or its inputs.

## Turn timing

Seconds; p95 uses nearest rank. With six samples per arm it is the maximum
observed value, not a reliable production tail-latency estimate. First text is
availability at the existing draft-ready event, not a measured browser display.

| Memory | Success | First text median | First text p95 | Full turn median | Full turn p95 |
| --- | --- | --- | --- | --- | --- |
| Off | 6/6 | 24.33 | 46.89 | 75.39 | 99.30 |
| On | 6/6 | 27.40 | 35.35 | 70.59 | 115.18 |

Memory lookup plus context construction took 15–23 ms (median 19 ms), with no
skipped exposures. This is small compared with the measured model waits.
Full-turn medians differ by 4.80 seconds between arms, but that is not a causal
estimate of memory impact. Pair-level outcomes go in both directions, generated
content differs, and this sample is too small for a reliable speed decision.

Paired deltas below are **on minus off**; negative time means the memory-on turn
finished earlier. The median paired full-turn delta is −7.98 seconds, while the
median first-text delta is +0.72 seconds. Neither establishes an improvement.

| Pair | First text delta, s | Full turn delta, s | Writer word delta |
| --- | --- | --- | --- |
| conversation/1 | +11.91 | +45.04 | +50 |
| conversation/2 | -3.82 | +15.21 | +12 |
| danger/1 | +1.75 | -47.54 | -25 |
| danger/2 | -18.53 | -23.79 | +0 |
| inspection/1 | -0.31 | +4.25 | +8 |
| inspection/2 | +1.80 | -20.20 | +10 |

## Role timing and critical path

Per-call median seconds; parallel times overlap and must not be summed.

| Role | Model | Memory off | Memory on |
| --- | --- | --- | --- |
| `choices` | `gemini-3.5-flash-lite` | 1.25 | 1.14 |
| `quest_evaluator` | `antigravity-preview-05-2026` | 35.54 | 26.12 |
| `state_evaluator` | `gemini-3.5-flash-lite` | 1.04 | 0.86 |
| `turn_planner` | `gemini-3.5-flash-lite` | 1.12 | 1.19 |
| `world_evaluator` | `antigravity-preview-05-2026` | 26.98 | 25.22 |
| `writer` | `antigravity-preview-05-2026` | 23.21 | 26.31 |

The writer dominates time to first available text. After it finishes, world,
state, and quest evaluation already run concurrently. Choices wait for state and
quest results, then can overlap a still-running world evaluation. The slower of
world evaluation and the quest/state → choices branch dominates completion.
Further generic parallelization of these three evaluators is already present
and cannot be counted as a new speed improvement.

Provider-reported token usage has an outlier: the memory-off danger/1 quest call
reports 40,280 input tokens for a 4,324-byte role input. Role input bytes exclude
system instructions and provider-internal work; usage accounting is retained
verbatim, not rewritten or equated with visible context length. The aggregate
input-token difference is therefore not evidence of a memory efficiency gain.
Writer outputs range from 107 to 166 words and two to three paragraphs. No blind
literary-quality or continuity review was performed.

## Diagnostics and validation

The optional owner-scoped metrics API exposes recorded attempt/role timing,
usage, repair and retry counts. Logs add role and attempt timing. It uses the
existing tables and requires no migration. Ownership, invalid IDs, unavailable
storage, snapshot reads, and raw-error exclusion are covered by tests.

Passed: Go formatting, vet, all backend tests, full race suite with the isolated
PostgreSQL database, repeated fake full-pipeline integration/cleanup, PostgreSQL
metrics integration, frontend typecheck/lint, all 45 frontend tests, and production
build. The frontend dependency audit still reports 14 pre-existing findings.

Pipeline timing excludes queue/config resolution, durable telemetry writes,
real SQL Canon commit, illustration jobs, and browser/SSE transport/rendering.
The API was verified through handler and database integration tests; a running
browser/application was not validated in this increment. Pipeline success alone
does not approve generated prose or justify changing model defaults.

## Next measured hypothesis

Target the expensive post-writer work first: determine whether a smaller,
structured evaluator request can preserve negative-case correctness while
reducing world/quest latency. Use the same paired pipeline cases for timing,
add frozen continuity/negative cases for acceptance, and compare actual request
counts and output quality before selecting a new default. Separately investigate
the provider usage outlier. Preserve the current preview and dependency ordering.
