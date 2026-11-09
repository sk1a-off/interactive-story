# Frozen lighthouse evidence replay — 2026-09-10

No production settings, prompts or routing changed. No Writer calls.
Artifacts: `tools/local-llm-benchmark/results/world-frozen-20260910-JnDqtN/report.json`.
Candidate: `backend/cmd/hybridbench/fixtures/world-newbeat-quotes-v2.md`.

Compared previous world candidate (corrected JSON examples, NOT the active
production prompt) against that candidate plus short contiguous newBeat quote
guidance. Names and established appearance may come from context, but quoted
evidence must come literally from the new beat. The quote need not repeat the
entity name. Seven scenes, two repeats, reversed prompt order on repeat two:
28 Flash-Lite calls, all completed without transport or validation errors.

- Previous candidate: 14/14, median 0.974 s, range 0.552–3.060 s.
- New guidance: 14/14, median 1.210 s, range 0.535–1.969 s.
- Frozen lighthouse: both prompts passed both repeats. Quotes described entry
  or the observed safe tower interior, rather than copying previousBeatText.

The old failure did NOT recur, so this comparison does not prove the additional
wording improves reliability or speed. Retain the earlier failing pipeline
report as evidence of intermittent failure; do not replace it with this success.

The fixture freezes exact saved Writer prose and its synthetic target. Request
context is reconstructed for the assay, so this is not claimed byte-identical
to the previous complete pipeline input. The report retains full compared inputs.
Regression scenes cover new entities, updates, duplicates, disproven rumors and
incidental crowds. Optional forge handling remains identical for both prompts.

Local regression verifies that the recorded old-context quote is rejected, a
literal newBeat entry quote passes, and empty output cannot count as a created
lighthouse. Targeted Go tests passed. These gates do not exhaustively verify
all described appearance details, and this is not a new full-pipeline run.

Next acceptance: fresh full-pipeline positive/negative world scenarios using the
candidate before switching world routing. Keep current active revision 22 and
the already-enabled fast state route unchanged until then.
