# Fast quest evaluator acceptance — 2026-09-09

Production settings unchanged. All model inputs were synthetic. Added
`-fixture-only` so full-pipeline benchmarks do not read real stories, timelines,
Reader views or Director instructions. This mode requires a target in each case;
database reads are limited to AI/prompt configuration. Canon uses in-memory capture.

Artifacts: `tools/local-llm-benchmark/results/quest-fast-gates-20260909-0wa0RE/`.

## Isolated role screen

Eight fixtures, two repeats: partial progress, memories, failed effort,
already-counted historical achievement, new historical achievement, completion,
terminal failure, and closing a parent together with its active child.

- Active prompt revision 21: 15/16 passed. One response mixed `operation=progress`
  with `status=completed, progress=100`; production validation rejected it.
- Added a regression test for that exact inconsistency.
- In-memory prompt candidate `cmd/hybridbench/fixtures/quest-fast-contract-v1.md`
  explicitly aligns operation/status/progress. It passed 16/16 on a fresh repeat.
  SHA-256: `b1b004dffce554f96f862b666bce87a63c7eaadcae455965d11e58c84d702767`.
- Candidate median call 1.146 s, range 0.890–35.452 s. Small stochastic sample;
  not a proof of stable one-second response or a paired prompt-effect experiment.

## Full-pipeline screen (original active prompt, not the candidate)

Writer/world/state remain Antigravity, Planner/Choices/quest use Flash-Lite.

1. `consume-ration-01`: passed event and resulting inventory gates; 332.703 s.
   Writer 32.899 s, quest 0.825 s, state evaluator 297.342 s. Main bottleneck
   remained outside the quest role.
2. `open-door-01`: pipeline returned without transport error, but acceptance failed;
   68.937 s. The required objective update was absent. Quest called twice,
   including semantic repair, and repeated the same rejected quote.

The door was narratively opened. Evidence quoted its motion and contact with a
stop, while the criterion was "Дверь архива полностью открыта". The conservative
five-character-stem evidence heuristic does not match "двери" to "дверь", and
the selected quote lacks the explicit open-state wording. The final paragraph
contains "в крайнем открытом положении", but neither quest call selected it.
This is a compatibility problem between evidence selection and the gate, not
proof that the narrated action failed. The pipeline then discarded quest changes.

Decision: no production activation. The clarified prompt still needs a complete
pipeline pass. Freeze/replay the door output to investigate evidence selection
without rerunning Writer; do not weaken false-completion protection blindly.
State evaluator latency is a separate high-priority bottleneck. Faster quests
alone cannot guarantee a faster completed turn.

Targeted Go tests passed. Test reports and prompt candidate are local artifacts;
no production prompt revision was created or activated.
