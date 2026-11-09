# World evidence contract experiment — 2026-09-10

Production remains unchanged: active prompt revision 22, fast state enabled,
world and quest routing unchanged. Artifacts:
`tools/local-llm-benchmark/results/world-prompt-20260910-RROUKc/`.

## Balanced frozen-scene comparison

Six synthetic scenes, two repeats, baseline/candidate order reversed on repeat 2
(24 calls). Same Flash-Lite model and inputs; only the two JSON example objects
gain an evidenceQuote field. Other active prompt instructions are preserved.
Candidate saved as `cmd/hybridbench/fixtures/world-evidence-contract-v1.md`.

The forge fixture now explicitly allows one optional location named Кузница,
because the scene establishes a recurring forge. Boris is still required;
unrelated entities and duplicate forge outputs fail. The same amended checker
is applied to both prompt arms. This is not exhaustive visual-grounding QA.

- Baseline: 9/12 passed; median 0.788 s, range 0.522–1.369 s.
- Candidate: 12/12 passed; median 0.926 s, range 0.583–3.173 s.
- All three baseline failures omit evidenceQuote (lighthouse twice, redundant
  known character in a disproven-rumor scene once). No transport failures.

The comparison establishes improved schema compliance on these fixtures, not
universal quality or full-turn speedup.

## Fresh full-pipeline acceptance

Two synthetic scenes with Writer/quest on Antigravity and world/state/planner/
choices on Flash-Lite. Active state prompt and journal isolation retained;
world candidate applied only in memory. No real stories read or written.

1. Boris: passed required world event gate. Total 45.512 s, world 1.437 s.
   Manual inspection confirms character named Борис, age 40, blacksmith role and
   expected ongoing repair relationship in the committed in-memory event.
2. Lighthouse: failed required location event gate. Total 54.491 s, world 1.156 s.
   Prose successfully enters and explores the tower. Model evidence quote is
   copied from previousBeatText rather than newBeat; production correctly
   rejects the world batch and no location event is emitted. No model repair.

Final result: 1/2 pipeline cases. Do not activate the world prompt/model yet.
Next freeze this lighthouse output and test short contiguous newBeat evidence,
separating context-derived entity identity/appearance from proof of the newly
observed event. Do not weaken the evidence boundary or rerun Writer unnecessarily.

All Go tests passed after benchmark changes. Added a narrow prompt patch test;
no live database integration or UI generation is claimed by this test suite.
