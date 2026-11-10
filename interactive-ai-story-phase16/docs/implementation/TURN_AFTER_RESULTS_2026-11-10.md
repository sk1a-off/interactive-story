# Full-turn comparison — stage 3.10

## Outcome

The complete after run succeeded in **12/12 turns**, but **does not demonstrate
a speed improvement**. In these historical versus current observations, first
available text and full-turn completion were slower. Defaults are not switched
to a faster-looking unvalidated chain.

The useful runtime correction was quota recovery: a long Retry-After hint no
longer blocks every key and model on the origin. Compatibility work also fixed
double-appending the path of a full local `/v1/chat/completions` address.

## Comparable setup and evidence

- After run: 2026-11-10 06:22:20–06:49:04 UTC; 12 turns, three frozen scenarios,
  two repeats and six memory-off/on pairs, ordering seed `20261109`.
- AI revision 26 (`68c83eeb-d301-4db9-a58d-9a05a111eb4e`) and prompt revision 25
  (`905196f5-c502-4170-953a-c7f3aeaa9982`) match the before run.
- Same fixture hash, stored Safe Hybrid routing, turn planner, preview, fast
  state and journal isolation. Quality profile; schemas/native writer streaming
  are off. No active production configuration was changed.
- Real USER2 snapshot `23f65b34cf7632032061f5cc66c14714e6d4cee4`, Python 3.14.8,
  all installed package versions and CPU/thread limits match the recorded
  baseline. Lookup/context construction took 15–17ms, median 16ms; memory-on
  turns injected three documents, memory-off turns zero.
- The old prompt hash accidentally represented `{}` because Revision's fields
  are excluded from public JSON. Historical raw results are preserved unchanged.
  The corrected hash covers actual prompt/role-settings maps in all four
  affected benchmark modes. The comparator warns about the legacy hash and
  additionally checks pinned revision identities and all setup/row keys; it
  cannot reconstruct an independent historical content hash.

Raw evidence:
[before](benchmarks/turn-configured-baseline-2026-11-09.json),
[complete after](benchmarks/turn-configured-after-2026-11-10.json),
[machine comparison](benchmarks/turn-before-after-2026-11-10.json),
[environment/source fingerprints](benchmarks/turn-after-environment-2026-11-10.json).

## Timing

Seconds. There are six samples per arm; nearest-rank p95 is the observed maximum,
not a reliable production tail estimate. First text means pipeline draft-ready
availability, excluding browser rendering.

| Memory | Success before → after | First-text median | Full-turn median | First-text p95 | Full-turn p95 |
| --- | --- | --- | --- | --- | --- |
| Off | 6/6 → 6/6 | 24.33 → 29.02 | 75.39 → 90.73 | 46.89 → 60.88 | 99.30 → 187.32 |
| On | 6/6 → 6/6 | 27.40 → 29.39 | 70.59 → 100.38 | 35.35 → 55.98 | 115.18 → 373.58 |

Across all twelve balanced off/on observations, first-text median changed from
26.10s to 29.02s and completion from 71.86s to 96.79s. Matching by scenario,
repeat and memory arm gives median after-minus-before completion deltas of
**+9.41s off** and **+40.70s on**; these are different statistics from subtracting
arm medians. All twelve success pairs are retained.

This is not a randomized causal estimate. Live provider load/quotas, generated
outputs and reported token usage differ; the after implementation also observes
backoff hints. Regression checks briefly ran during the remote-model wait and
are disclosed in the environment record.

## Calls, failures and dominant waits

| Measure | Before | Complete after |
| --- | --- | --- |
| Logical role calls | 72 | 72 |
| HTTP POSTs / GETs | 72 / 0 | 75 / 0 |
| HTTP statuses | 72 × 200 | 72 × 200, 2 × 429, 1 × 503 |
| Structured repairs | 0 | 0 |
| Model escalations | 0 | 1 |
| Choice / intent fallbacks | 0 / 0 | 0 / 0 |
| Four-choice turns | 12/12 | 12/12 |

One 429 carried Retry-After **5855s**: the rejected credential/model was skipped
and the writer recovered through another key. Another carried **22s**, which
the bounded short shared backoff honored. One fast planner 503 escalated to
Antigravity. Successful turns therefore do not mean the provider returned only
successful responses.

The largest world-evaluator calls took **334.28s**, **232.31s** and **158.61s**,
each with one HTTP POST. They were not additional local scheduling gaps. Median
writer time changed from 23.21/26.31s (off/on) to 26.55/28.06s; median quest
evaluation from 35.54/26.12s to 27.57/35.75s. These changes vary by arm and do
not establish a general improvement.

Provider-reported input/output totals were 338,363/9,165 before and
749,659/10,926 after. One after world response reported 255,094 input tokens for
a 4,545-byte application role input. Usage may include provider-internal work or
other accounting; it is retained verbatim and is not equated with visible prompt
length or complete usage of rejected/escalated attempts. Outputs ranged from
107–166 words and 2–3 paragraphs before, versus 101–189 words and 2–6 paragraphs
after. No blind literary-quality approval is inferred from pipeline success.

## Interrupted diagnostic run is not mixed into the comparison

The first after attempt stopped after six rows: four successful turns, one
writer quota failure and one cancelled turn waiting 179.7s without sending a
POST. It exposed the old origin-wide long cooldown. That report remains
[preserved](benchmarks/turn-after-interrupted-2026-11-10.json), with verified
fixture cleanup. The comparator rejects it as incomplete. The second complete
run starts after the quota-scope fix; neither run overwrites or hides the other.

## Verification and operating state

Both benchmark reports record fixture cleanup; the final fixture owner is also
absent on an independent SQL check. Production metadata was read-only, and the
working database remains at migration 35 until the user runs migration commands.
The isolated test database is at migration 40. No benchmark stories or Canon
were committed to the working database.

Go formatting/vet/full tests, full race checks with isolated PostgreSQL and server
build passed. Final queue/timeout additions approved by the user were also
reviewed and passed a fresh uncached full race suite, including cancellation
terminal replay. Frontend clean install/typecheck/lint, 57 tests and production
build passed; 14 pre-existing npm audit findings remain.

Durable queue/config resolution, telemetry persistence, real SQL Canon commit,
browser rendering and image generation are outside this timing experiment.
Queue/timeout recovery is documented separately in
[GENERATION_RECOVERY_2026-11-10.md](GENERATION_RECOVERY_2026-11-10.md).
An actual selected GGUF, manual full-browser playthrough and Perchance round trip
remain runtime acceptance gaps. The temporary benchmarking USER2 process is not
running in the current restored environment.

## Decision and next work

Keep current quality/safe-hybrid defaults; do not label this result an acceleration.
The request-limit correction prevents one observed class of prolonged quota
blocking, while managed-agent response time still dominates the measured tail.
Stage 4 can now inventory/rework prompts and test smaller world/quest contracts
against frozen negative cases plus this same turn harness before any activation.
