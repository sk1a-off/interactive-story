# Fast quest model screen — 2026-09-09

Motivation: prioritize latency rather than adding context. The classified ledger
has never been enabled in the production pipeline. Latest ten stored jobs show
quest_evaluator mean 78.6 s (Antigravity); these are historical heterogeneous
workloads, not a concurrent control arm for this experiment.

Run: `tools/local-llm-benchmark/results/quest-fast-20260909-r7XcwF/report.json`.
Model `gemini-3.5-flash-lite`, active prompt revision 21, two repeats of five
synthetic fixtures, full unpruned context, no ledger. No config or Canon writes.
The existing stopped PostgreSQL container was started for configuration reads.

Results: 10/10 passed production evidence/domain gates plus fixture outcomes.
Latency range 753–1313 ms; median 1073.5 ms. No transport errors or repairs.

- Partial progress retained (50, active).
- Memory and failed attempts produced no changes.
- Newly achieved progress with historical context retained (66, active).
- Already-counted fixture returned a redundant progress=33 in both repeats;
  the existing evidence preparation correctly dropped this unchanged value.
  Thus 10/10 describes post-validation outcomes, not perfectly minimal raw output.

Conclusion: promising fast-model candidate without context loss. This small
screen does not establish full-story latency or broad quality equivalence.
Prior Antigravity timings are historical comparisons only. Production remains
unchanged. Before enabling, test completion/failure, active child stages, creation,
ambiguous evidence, and complete pipeline turns. Keep Writer unchanged.

Reproduce the mode with `hybridbench -quest-context-suite -quest-model-screen
-quest-suite-live -quality-model gemini-3.5-flash-lite -output NEW_PATH`.
