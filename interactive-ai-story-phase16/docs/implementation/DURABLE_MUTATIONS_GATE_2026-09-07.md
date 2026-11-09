# Crafted durable mutation gate — 2026-09-07

## 2026-09-08: user-approved activation

The user requested applying the settings after the remaining-gates run finished
30/30 (Planner 15/15, Legacy 15/15). Activated prompt revision **19**
(`0f4de257-be2b-4379-909c-2a175066c444`) from revision **18**
(`3345c35b-6acb-45be-8182-b32551890dba`). Only `quest_evaluator` changed, using
the exact tested snapshot with SHA-256
`e212f92f607f0efd2027c386727192389aa37d9d0aa425dda402c24134c03db6`.
All other prompts and role settings were verified unchanged. Revision 18 is
retained for rollback; `cmd/questpromptapply` checks the expected revision and
snapshot hash before creating/activating the new revision.

Local `.env` now sets `GENERATION_TURN_PLANNER_V1=true`. `make backend-dev`
exports that switch; `make dev` explicitly loads the root `.env` for Compose.
Resolved Compose configuration and Make environment both show true; comparison
of Compose configurations found no other resolved setting changed. The code's
default remains OFF when no deployment flag is supplied.

Only PostgreSQL was running during this change. Backend was not started, so
Planner becomes effective on the next launch through these entrypoints. No
story generation or Canon mutation was performed. Targeted Go tests and diff
checks passed. Rollback: reactivate prompt revision 18 in Prompt Studio, set the
local flag to false, and restart backend. Earlier OFF/pending notes below are
historical checkpoints, superseded by this explicit user-approved activation.

## 2026-09-08: revised parent-progress prompt — live repeat results

The explicitly requested three-pair run completed with exit 0. Planner **3/3**,
Legacy **3/3** passed the revision-2 gate, with the original objective remaining
active and exactly one partial-progress update per turn. Planner progress values
were 50/50/50; Legacy 40/45/45. All six updates retained the original criterion.
There were 42 model calls total, no repairs, and one Writer call per turn.

| Pair | Planner time | Legacy time |
| --- | --- | --- |
| 1 | 66.076 s | 79.052 s |
| 2 | 57.273 s | 326.123 s |
| 3 | 78.927 s | 77.233 s |

The second Legacy turn included a 294.749 s state evaluator call. This remains
a latency outlier, not a semantic failure. All reports record base prompt
revision 18 and the same local quest override SHA-256, verified against the
saved file: `e212f92f607f0efd2027c386727192389aa37d9d0aa425dda402c24134c03db6`.

Artifacts: `tools/local-llm-benchmark/results/partial-progress-20260908-080818-YQB6gN/`,
`pair-1.json` through `pair-3.json`, plus compact per-pair summaries and the
exact prompt/fixture snapshots. Local script tests and build passed before
model calls. Database prompts and Canon were not changed.

This confirms three successful repeats of the targeted positive scenario in
each mode; it does not establish broad quality or absence of false progress on
negative cases. Keep the production prompt unchanged and Planner default OFF
until that remaining coverage is checked.

## 2026-09-08: parent-progress prompt candidate preparation

The repeat in `partial-progress-20260908-072820-63YckL/pair-1.json` exposed the
same omission in Planner: only a new child stage was created. Planner failed
the stronger gate; Legacy retained parent progress 50 and passed. Planner took
328.846 s, including a 287.161 s state evaluator call; Legacy took 125.72 s.

The saved child event is now a compact regression fixture in
`backend/cmd/hybridbench/fixtures/recorded-partial-progress-miss.json`. The test
rejects child-only output and accepts synthetic partial parent updates without
requiring exactly 50. This tests the gate, not live model compliance.

The local quest evaluator prompt now prioritizes evidenced updates to existing
goals before optional stages, explicitly says that a child does not replace
parent progress, and prohibits progress based only on intention, failed effort
or historical evidence. No automatic percentage or new repair call was added.

`check-partial-progress.sh` defaults to three pairs (six turns, normally 42
model calls; repairs can add calls). It snapshots the revised quest prompt and
passes `-quest-prompt-file` to both modes. This replaces only the quest prompt
in memory; the database prompt revision is unchanged. Reports include the
override path and SHA-256, alongside the base database revision. This is
Planner/Legacy comparison with the new prompt in both, not a randomized
old-prompt/new-prompt experiment.

Local benchmark, bootstrap and generation tests, script syntax/help and diff
checks passed. No new live calls were made for this change. The candidate is
not verified to resolve the omission; the production active prompt was not
activated or edited, and Planner remains default OFF.

## Repeated resources / partial-progress comparison and stricter revalidation

Completed 18 live turns: three repeats of each resource fixture in each mode,
prompt revision 18, `durable-resources` revision 1. Inputs were synthetic,
Director instructions cleared, and Canon captured in memory. All original
checks passed: Planner 9/9, Safe Hybrid Legacy 9/9. Original report remains
unchanged: `tools/local-llm-benchmark/results/durable-resources-paired-3x-20260907.json`.

| Case | Planner times (s) | Legacy times (s) | Recorded state |
| --- | --- | --- | --- |
| spend-coins-01 | 132.348 / 61.156 / 103.758 | 59.992 / 85.588 / 60.249 | 6/6: original currency active, 10 → 7 |
| consume-last-ration-01 | 45.996 / 37.934 / 72.098 | 66.162 / 73.522 / 49.924 | 6/6: original item inactive, 1 → 0 |
| partial-wagons-01 | 255.764 / 126.319 / 58.826 | 97.396 / 127.137 / 47.008 | All keep original objective active; progress 50 in Planner 3/3 and Legacy 2/3 |

Planner used 54 model calls versus Legacy 72, with nine Writer calls each and
no repairs or escalations. Aggregate median time was **72.098 s vs 66.162 s**;
this series does **not** show a Planner latency improvement. Per-case medians
(Planner / Legacy): coins 103.758 / 60.249 s, ration 45.996 / 66.162 s,
wagons 126.319 / 97.396 s. The slowest Planner turn included a 220.127 s
state evaluator call. Candidate-first ordering and provider variability remain
confounders; fewer calls do not guarantee a faster turn.

### Missing progress exposed by a stronger gate

In Legacy wagon repeat 2, the quest evaluator emitted only a new active child
task, `Остановить вторую повозку`, at progress 0, parented to the original goal.
It omitted the original goal's progress update despite the text describing the
first wagon as secured. This was not a rejected completion quote: the update
was absent from the model proposal, and no repair ran. The original safety-only
validator legitimately passed because no goal was wrongly completed or failed.

Fixture revision 2 now also requires exactly one update for the **original**
objective UUID with status active and progress in **1..99**. A range avoids
requiring an arbitrary precise estimate of 50. Missing updates or child-task
creation alone fail. Exact-progress expectations remain supported; ranges and
exact values cannot be mixed. Deterministic tests cover missing progress,
child-only output, bounds, malformed range settings and source preservation.

The new `-revalidate-report` mode rechecks saved events locally without loading
credentials, connecting to the database or calling models. It requires a new
output file, records source report/revision and revalidation time, and checks
case names/actions and explicit fixture targets. It does not regenerate content
or prove equivalence if fixture inputs were changed. Here the target and action
were unchanged; only expected outcomes were strengthened.

Revalidation result: **Planner 9/9, Legacy 8/9**, with only the missing original
objective progress failing. The command saved its report and exited nonzero as
expected. This is a retrospective stricter check of the same 18 turns, not 18
new live runs. Artifact:
`tools/local-llm-benchmark/results/durable-resources-paired-3x-20260907-revalidated-v2.json`.

### Text review and current decision

All six wagon texts were manually inspected: each secures the first wagon and
leaves the second unsecured; none automatically performs the deferred action.
All three Legacy texts additionally make the second wagon roll toward an edge,
while Planner keeps it unsecured and at risk. This is an observed narrative
pressure difference, not an automatically validated agency or quality verdict.

Full ordinary `go test ./...` passed after the benchmark changes. No production
generation behavior was changed in this step. Keep Planner default OFF: the
next work is partial-progress recall coverage/repair and broader action-agency,
scene-transition and unsupported-completion tests. The earlier resource
single-run notes and mutation comparison below remain historical evidence.

## Repeated Safe Hybrid Legacy / Planner comparison

Completed `-runs 3 -profile baseline-safe -planner-mode flash-lite
-compare-safe-legacy` against `durable-mutations-v1.json` (revision 1), using
database prompt revision 18. The new comparison mode preserves the actual
Safe Hybrid Legacy routing, without Planner or Extractor; the existing
`-compare` mode still means quality-only with the same optional stages.
Both groups used the same synthetic fixture targets and prompt revision,
with Director instructions cleared and Canon events captured in memory.

| Case | Planner times (s), gate | Safe Hybrid Legacy times (s), gate |
| --- | --- | --- |
| consume-ration-01 | 54.099 / 84.769 / 67.172; 3/3 | 63.935 / 82.256 / 158.088; 3/3 |
| open-door-01 | 42.815 / 65.342 / 42.298; 3/3 | 88.648 / 100.628 / 75.317; 3/3 |

All 12 turns passed exact entity-ID, quantity/status/progress, state-invariant,
event-count and four-distinct-choice checks. Planner: 36 calls, no repairs.
Legacy: 50 calls, two quest evidence repairs. Both: six Writer calls,
no escalations. Baseline traces confirm Flash-Lite action interpretation,
pacing and choices; Antigravity Director, Writer and evaluators. Planner traces
confirm Flash-Lite turn planning/choices and Antigravity Writer/evaluators.

Conventional sample medians (average of the two middle values for six runs):
Planner **59.721 s**, Legacy **85.452 s**. The report's nearest-rank `p50`
values are respectively 54.099 s and 82.256 s; these are a different statistic.
The slowest Legacy turn included a 127.113 s state evaluator call.
Candidate runs preceded baseline runs rather than randomized interleaving;
small samples, provider variability and different repair counts prevent a
general causal latency claim.

In Legacy door repeats 1 and 2, initial verbatim evidence used synonyms without
criterion-stem overlap. Each bounded repair returned a longer contiguous quote
and the expected completed/100 event was retained, with no Writer retry. These
are live end-to-end observations of the semantic repair path, supplementing the
isolated probe below. Quote expansion does not itself prove entailment: the
lexical guard remains heuristic and needs broader unsupported-completion tests.

Artifact: `tools/local-llm-benchmark/results/durable-mutations-paired-3x-20260907.json`.
The benchmark exited successfully after checking both groups. Full ordinary
`go test ./...`, targeted `go vet` (benchmark, generation, file secrets) and
`git diff --check` passed. The opt-in live repair test is not part of ordinary
`go test`; its separately observed results are recorded below.

**Decision:** the repeated gate passes for these two fixtures only. Keep
`GENERATION_TURN_PLANNER_V1` default OFF. At this checkpoint remaining work included repeated paired
resource/partial-success cases (now recorded above) and broader action-agency, scene-transition and
unsupported-completion coverage; this does not close the full workflow gate.

## Additional resource and partial-success fixtures

`durable-resources-v1.json` adds independent synthetic inputs and checks for
spending currency, exhausting the last item, and leaving a multi-part objective
active. `objectives_remain_active` rejects terminal objective events including
completion of a newly substituted objective; empty mutations are permitted in
this negative control, so it measures safety rather than progress recall.

Live Planner results, one repeat per case, prompt revision 18:

| Case | Time | Observed mutation | Gate |
| --- | --- | --- | --- |
| spend-coins-01 | 62.540 s | currency 10 → 7, active, expected UUID | PASS |
| consume-last-ration-01 | 100.741 s | item 1 → 0, inactive, expected UUID | PASS |
| partial-wagons-01 | 73.332 s | objective active, progress 50 | PASS |

Three runs, 18 calls, one Writer per turn, no repairs or escalations. Evidence
quotes describe the remaining seven coins, exhaustion of provisions, and only
the first completed portion of work. No Canon writes. Artifact:
`tools/local-llm-benchmark/results/durable-resources-planner-20260907.json`.
These are single-run observations, not the repeated paired Legacy/Planner gate.

## Initial paired mutation observations

Two synthetic fixtures, one run each per profile, prompt revision 18.
Both profiles use Antigravity Writer and separate evaluators; only planning
differs. Fixture targets replace live context and Director instructions.
Events are captured in memory; no Canon writes.

| Case | Legacy | Flash-Lite Planner | Exact mutation |
| --- | --- | --- | --- |
| consume-ration-01 | 75.389 s, PASS | 60.423 s, PASS | Existing ration quantity becomes 1 |
| open-door-01 | 64.688 s, PASS | 81.944 s, FAIL | Expected completed/100 objective event absent |

Legacy: 16 calls, 2/2 valid. Planner: 12 calls, 1/2 valid.
Neither profile used repairs or escalations. This sample is insufficient for
latency conclusions or default-ON rollout.

## Missing completion diagnosis

Planner chose the requested action. Writer explicitly opened the door fully.
The quest evaluator proposed completion for the correct UUID and supplied this
verbatim quote:

> Алекс распахнул створку до самого упора, пока она с глухим щелчком не зафиксировалась на резиновом ограничителе у плинтуса.

Criterion: `Дверь архива полностью открыта`.

`prepareObjectiveEvidence` requires `quoteSupportsCriterion`, which compares
word stems. The quote uses synonyms and has no matching stem; completion is
discarded. The pipeline succeeds with zero objective events, but the new exact
expectation correctly fails the benchmark. `semantic:objective_state=true`
alone cannot detect this because the empty mutation set violates no state rule.

This is a conservative evidence false negative, not evidence of Planner agency
failure. Do not remove verbatim evidence validation or broaden acceptance to
the whole Beat. Next: bounded evaluator repair with explicit rejection feedback,
plus negative tests for unrelated or unsupported completion.

## Repeat after bounded quest evidence repair

The Planner door case passed in 42.998 seconds: six calls, Writer once,
quest evaluator once, zero repairs/escalations. The exact objective completion
expectation passed. Artifact:
`tools/local-llm-benchmark/results/durable-mutations-planner-repair-20260907.json`.

This repeat did not exercise semantic repair: the evaluator supplied acceptable
evidence on its first call. Repair behavior is covered by deterministic tests
reproducing the earlier rejected synonym quote, accepting an alternate direct
quote, rejecting historical evidence and criterion substitution, and asserting
one Writer call and two quest evaluator calls. Live repair acceptance remains
unverified by that repeat; this single successful repeat does not close the overall Planner gate.

### Explicit live repair probe

An opt-in test now directly invokes the same production repair helper against
Antigravity with fixed synthetic input. Both cases passed:

- Recorded false negative: 45.789 s, one repair call; returned the contiguous
  quote `Дверь была открыта, и архив лежал перед Алексом`. Evidence and domain
  validators accepted completion for the expected UUID at progress 100.
- Unsupported completion (door stayed closed): 10.878 s, one repair call;
  returned `changes: []`, producing no mutations.

Writer calls: zero. Canon writes: zero. Prompts came from the embedded repository
quest evaluator contract, not the active database prompt revision. These two
observations prove this repair path was exercised live; they are not a broad
semantic-quality gate. Raw outputs and timings are saved in
`tools/local-llm-benchmark/results/objective-repair-live-20260907.json`.

`TestLiveObjectiveEvidenceRepair` is skipped by ordinary test runs and requires
explicit `STORY_REPAIR_LIVE=1`, workspace root and configuration UUID.

## Initial comparison artifacts

- `tools/local-llm-benchmark/results/durable-mutations-legacy-20260907-v3.json`
- `tools/local-llm-benchmark/results/durable-mutations-planner-20260907.json`

Legacy reports without `v3` are infrastructure failures (CA certificate setup),
not model-quality observations. The local credential file did not contain the
active key; the successful runs used the existing Docker secret volume read-only
and the host CA bundle. Benchmark credential loading now supports read-only files.
