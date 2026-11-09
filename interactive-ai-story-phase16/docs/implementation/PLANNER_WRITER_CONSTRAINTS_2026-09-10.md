# Planner → Writer constraint assay — 2026-09-10

Artifacts: `tools/local-llm-benchmark/results/constraint-probe-20260910-lJ8zKV/`.
No active prompts, models or user stories changed. Synthetic unchanged-archive
fixture; no Canon commits. Active prompt revision 22.

## Planner: first comparison

Actual LLMTurnPlanner input construction, normalization and structural gates;
Flash-Lite, two repeats with reversed baseline/candidate order.

- Baseline: structurally valid 2/2, but both immediate goals direct discovery of
  hidden details or changes despite the unchanged scene constraint.
- Candidate adding constraint-preservation guidance: malformed 2/2. It returns
  a rewritten context or narrative rather than the required plan, although its
  immediate goals describe bounded observation. Not usable as-is.

## Writer: controlled goal intervention

Same Writer prompt/model (Antigravity), intent and context. Only goal changes:
the recorded drifting goal versus a manually authored constraint-preserving
goal. Two repeats in balanced order, four Writer calls, no repair calls.
Manual assessment checks durable damage/clues/hidden activity, entering the
building and starting interactions; sensory staging/time passage is allowed.

- Drifting goal: both outputs invent fresh damage, suspicious signs or hidden
  activity (2/2 violations); durations 17.521 and 23.142 s.
- Constraint-preserving goal: both preserve the archive without the prohibited
  discoveries (2/2 pass); durations 27.671 and 15.302 s.

This supports the effect of immediate-goal direction on this scene, not universal
causality or improved latency. The good Writer inputs use hand-controlled goals,
NOT plans from the candidate model; this is not end-to-end candidate acceptance.
Writer still adds atmospheric/architectural staging, not exhaustively audited.

Active Writer prompt also requires at least two observable state changes per
beat and encourages escalation on repeated actions. This is a potential competing
instruction, but it was held unchanged; changing Writer prompt was NOT tested.
The novelty-rewrite path was not exercised either.

## Planner: explicit output contract follow-up

`planner-contract.json`: four new Planner calls, no additional Writer calls.
Baseline again yields discovery-seeking goals (2/2 semantic violations).
Candidate with explicit schema yields acceptable normalized plans (2/2), but
one RAW response copies the example placeholder `preserve player action` into
intent. Existing intent normalization replaces both intent and immediateGoal
with the user's action. Do not claim perfect raw-output compliance.

Candidate timings 0.881 and 0.995 s. Saved snapshot:
`cmd/hybridbench/fixtures/planner-constraints-contract-v1.md`.
This candidate is NOT activated. Its literal-placeholder copying remains a
regression target, and its downstream Writer behavior remains unverified.

Next: remove ambiguous example placeholders, test both observation constraints
and actions that genuinely require progress, then run the candidate Planner →
Writer chain. Avoid imposing a universal ban on narrative change. Preserve the
failed reports rather than replacing them with successful repeats.

Targeted Go tests passed. Model semantic outcomes above are manual assessments,
not automatically graded quality guarantees; tool exit status alone does not
mean semantic acceptance.

## Prompt-source correction discovered in the next turn

The active revision did not contain turn_planner text; the client resolved its
baseline from the embedded catalog. The original assay read the missing map
entry as an empty string when constructing its candidate. Thus the first
candidate REPLACED the full embedded prompt with a constraint-only fragment;
it did not append guidance to the effective baseline. This can explain the
malformed outputs and confounds that prompt comparison. The raw reports are
retained unchanged. The separate controlled Writer-goal comparison is unaffected.

The assay now resolves the same embedded fallback as the client and records its
source. New planner-constraints-v2 starts from the complete embedded contract,
preserves lifecycle/location rules and replaces literal example values with
field/type descriptions. Do not activate the earlier fragment candidate.
