# Quest evidence and fast state screen — 2026-09-09

Production routing and active prompt revision 21 were not changed. All live
model payloads were synthetic; full pipeline used `-fixture-only` and in-memory
Canon. Existing PostgreSQL was started after both initial launches failed before
model calls because the database was stopped.

Artifacts: `tools/local-llm-benchmark/results/quest-state-fast-20260909-k289M1/`.

## Frozen quest evidence

`quest-fast-outcome-v2.md` adds outcome-selection guidance to the prior contract
candidate. The saved door Writer text is embedded as a regression fixture; a
second fixture describes imagined success followed by an unsuccessful attempt.
No Writer calls were needed for this screen.

Twenty calls, 16 passed. Median 1.320 s; range 0.813–23.778 s.
Both actual door completions passed: the model selected the explicit final open
state. Three responses incorrectly marked a recoverable unsuccessful attempt as
terminal failure (one wagon attempt, both imagined-door repeats). These passed
production proposal validation but failed the fixture's semantic expectations.
One already-counted historical achievement used evidence outside the new beat
and was rejected by production validation. Do not activate this candidate.

Local regression tests verify that the original door movement quote remains
rejected and the explicit final-state quote is accepted. Protection was not
weakened. These tests do not prove general semantic correctness of the gate.

## Fast state: two full synthetic pipeline runs

Writer/world remained Antigravity; planner/choices/quest/state used Flash-Lite.
Quest used the in-memory outcome-v2 candidate. This is a functionality screen,
not a controlled timing comparison against the earlier configuration.

| Case | Total | State | Writer | World | Required journal update |
| --- | ---: | ---: | ---: | ---: | --- |
| Spend 3 of 10 coins | 128.592 s | 2.809 s | 25.052 s | 102.457 s | Missing |
| Consume last ration | 24.937 s | 1.162 s | 14.806 s | 9.333 s | Missing |

Both runs returned prose and choices without transport errors, but both failed
acceptance. State output reported the correct remaining coin balance of 7 and
removal of the consumed ration, but used `evidence` instead of `evidenceQuote`.
`prepareJournalEvidence` therefore rejected the changes before journal events
were produced. No repair calls occurred. `semantic:journal_state=true` alone is
insufficient: absent updates pass that limited consistency check, while the
required-event assertions correctly failed.

Read-only inspection of active revision 21 confirmed a prompt contradiction:
the JSON example requests `evidence`, while an appended compatibility section
requires `evidenceQuote`. The repository's `backend/prompts/v1/state_evaluator.md`
already uses the correct example, but that is not the active database prompt.
The screen therefore does not establish that Flash-Lite cannot perform state
evaluation. Fix the isolated prompt contract and replay these saved outputs
without regenerating prose before repeating a full pipeline acceptance run.

World latency remains a separate bottleneck; fast state alone cannot guarantee
a fast completed turn. No model/prompt activation is justified by this result.

## Verification and next step

Targeted Go tests passed. `go test ./...` passed with local test-server permission;
the sandbox-only run failed because narration's httptest listener was forbidden.
This does not constitute a separate live PostgreSQL integration test run.

Next: align the state prompt example with the evidence contract in an isolated
override; replay the frozen coin/ration texts. Independently distinguish a failed
attempt from irreversible quest failure and retest the negative quest fixtures.
Only then consider production activation. Preserve Writer and full history.

## Follow-up: frozen state replay (completed)

Added `-state-replay-report` (requires `-fixture-only`) and a pure
`CheckJournalProposal` wrapper over the existing production evidence/domain
gates. No Writer, world evaluator, timeline reads, or persistence is involved.
Inputs are reconstructed from the synthetic case targets and saved Writer texts;
they are identical between prompt variants, but not claimed byte-identical to
the original pipeline requests. Full replay inputs, outputs, prompt texts and
source SHA-256 are saved in each report.

Each experiment made eight calls: two scenes, two repeats, baseline/candidate
order reversed on the second repeat. Assertions require the existing coin entry
to end at active/7 and the ration entry at inactive/0 after production
normalization. Missing changes cannot pass. Other new entries receive production
validation, not exhaustive semantic acceptance testing.

- `state-contract-replay.json`: changed only the schema example from `evidence`
  to `evidenceQuote` with a verbatim-quote description. Baseline 0/4; candidate
  3/4. The remaining failure concatenated two passages with an invented ellipsis.
  Candidate durations: 2.399, 3.094, 45.998, 2.147 seconds.
- `state-shortquote-replay.json`: retained that correction and added explicit
  contiguous short-quote guidance, forbidding ellipses and concatenation.
  Baseline 0/4; candidate 4/4. Candidate durations: 1.310, 1.077, 1.727, 1.442
  seconds; median 1.376 seconds. The unit completed with exit status 0.

All eight baseline calls across these runs failed production validation. The
second candidate is promising for these two frozen positive cases only, not a
general quality or stable latency guarantee. The 45.998-second outlier from the
first experiment remains relevant. Raw `remove` responses may say quantity 1;
the production operation correctly normalizes them to inactive/0. This is not a
raw-output correctness claim.

Regression tests cover both expected mutations, rejection of the legacy evidence
field, non-verbatim evidence, and empty changes. Targeted tests passed after the
final CLI addition; the full Go suite passed before that small flag addition.
No active prompt revision or model routing was changed. Next acceptance step:
full synthetic pipeline with this state-only prompt override, plus negative
inventory cases (intent, observation, failed purchase). Quest failure handling
and world latency remain separate unresolved issues.
