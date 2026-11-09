# Quest context experiment — 2026-09-08

Status: experimental benchmark only. No server or active prompt changes.

## 2026-09-09: source-linked ledger prototype

Added benchmark-only `questInputWithLedger` (`quest-ledger-shadow-v1`). It
projects stored objective creation/update payloads into an explicit historical
ledger, with event ID, timeline ID and sequence for every record. It does not
infer achievements or summarize constraints using an LLM.

The caller must supply a trusted event prefix and snapshot. Missing/gapped,
reordered, mixed-timeline, duplicate-ID or malformed events cause an unchanged
input fallback. Current objective progress, status and criteria must match the
latest corresponding source event; mismatch also preserves the original input.

All original input fields remain intact, including history, cast and visuals.
Even a full event prefix does not prove that all prose conditions are present in
objective events. The ledger is historical context, not evidence for new progress.

Local tests confirm the two wagon histories yield different ledger evidence,
retain all original fields, preserve exact payloads/source references and fail
closed on invalid sources. Fixtures use explicitly synthetic source events.

Boundary: no production event reader integration, new model run, context
reduction or latency gain is claimed. This is a tested projection building block,
not an activated optimization. Next: obtain pinned production event snapshots
read-only, measure objective/evidence coverage, represent historical constraints
and reversals, then evaluate the ledger with the original history still present.
History removal requires a separate completeness gate; never add a model-supplied
`complete=true` flag as proof.

## 2026-09-09: information-loss gate blocks activation

Added `TestPrunedContextLosesAlreadyCountedAchievement` with two synthetic
histories. Both have three wagons, a parent objective at 33 percent, the same
current state and the same new beat securing the wagon at the gate.

- History A: the wagon at the gate was already secured and counted yesterday;
  its ropes remained intact. Expected: no progress.
- History B: yesterday's counted wagon was at the wall; the gate wagon was
  unsecured. Expected: new partial progress, without completion.

Only `recentBeats` distinguishes them. Full inputs are 10377 bytes each but
different. Pruning produces byte-identical 1422-byte inputs with opposite gold
expectations. Production evidence/domain checks plus fixture-specific outcome
checks accept the correct responses and reject the swapped outcomes.

This passing regression test proves a failure of the proposed representation,
not safety of the optimization. No model can reliably distinguish these two
cases from identical inputs. Extra paid calls cannot resolve missing information;
no live run was started for this counterexample.

Decision: do NOT activate `quest-pruned-v1`. Preserve relevant history until
counted achievements and historical constraints have a reliable, source-linked
representation. A future compact variant must preserve the distinguishing fact
and pass both outcomes, alongside broader history-dependent cases. A blanket
deletion of old prose is not a safe context strategy.

## First live pilot

User explicitly approved Google AI submission of synthetic data. Completed
`partial-wagons-01`, prompt revision 21, Planner flash-lite, normal quest model
antigravity-preview-05-2026. Report: `/tmp/quest-context-pilot-QDWWZE/report.json`
(temporary location). No database story writes or setting changes.

| Measurement | Baseline | Pruned |
| --- | ---: | ---: |
| Quest latency | 146543 ms | 51849 ms |
| Role input JSON bytes (excludes system prompt) | 6304 | 6247 |
| Provider-reported input tokens | 306755 | 6103 |
| Parent progress | 40, active | 45, active |

Both outputs cite the same verbatim evidence of the first secured wagon and
create an unresolved child for the second. Baseline pipeline validators passed;
candidate output was manually inspected, not run through the full Canon gates.
One Writer produced the shared 339-word beat. Total pilot wall time was 226597 ms,
including the extra diagnostic call. There were six normal calls plus one probe.

Conclusion: harness works, but speedup is NOT established. Removed fields were
empty in this small fixture: only 57 bytes (0.9 percent) were removed. Fixed
baseline-first order and one sample cannot attribute the latency difference to
context size. The enormous usage difference cannot be interpreted as a reduction
in serialized context. Investigate provider usage semantics separately; do not
equate total_input_tokens with unique input context or billing without verification.

Before more paid runs: add a representative rich synthetic fixture, verify its
actual byte reduction offline, balance call order, and validate candidate changes.

`hybridbench -quest-context-probe` runs an extra quest_evaluator request on
the exact same written beat, action, objectives, prompt and model as the baseline.
Only context.visualBible, context.initialCast and context.recentBeats are removed.
All unknown fields, story constraints, previous beat, rules and resources remain.
The baseline response is the only response used by the in-memory pipeline.
Neither response is committed to the application database.

Reports place candidate timings, input byte count, provider token usage, output,
input hashes and errors under `calls[].questContextProbe`.
The containing call is the baseline. No full input or credentials are persisted.

Important limitations:

- This pilot always calls baseline first; provider warm-up/load can bias latency.
- Whole-run elapsed time includes the extra probe: it is NOT candidate turn latency.
- `sameJson` is exact structural equality, NOT semantic equivalence or quality.
- Candidate `changesArray` only checks shape; normal run validators assess BASELINE
  events, not candidate semantic correctness. Candidate output requires review.
- Small synthetic targets establish harness behavior, not long-story speedup.

Next acceptance phase: freeze representative long-story inputs, balance A/B and
B/A order, repeat at least three times, and run both outputs through the same
evidence/semantic validators. Cover partial progress, no progress, remembered
past achievement, completion, active child stages, failed attempts and constraints
introduced in older prose. Any new lost or invented progress blocks activation.
Compare per-role p50/p95 and bytes/tokens separately from first-text latency.

Do not activate from this pilot or claim speed improvement from one pair.
