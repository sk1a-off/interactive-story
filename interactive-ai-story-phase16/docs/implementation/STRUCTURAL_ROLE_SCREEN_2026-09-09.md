# Fast state, quest and world screen — 2026-09-09

No production configuration, model routing or prompt was changed. No Writer
calls or story/timeline reads. Active prompt revision 22, model
`gemini-3.5-flash-lite`, 24 synthetic fixtures with two repeats (48 calls).
Artifacts: `tools/local-llm-benchmark/results/structural-screen-20260909-B2oun3/report.json`.
The report records all fixtures, raw outputs, role prompt hashes and timings;
it is checkpointed after every call. All calls completed without transport errors.

| Role | Strict assertions passed | Median | Range |
| --- | ---: | ---: | ---: |
| State | 16/16 | 0.975 s | 0.570–5.043 s |
| Quest | 15/20 | 1.066 s | 0.754–1.509 s |
| World | 7/12 | 0.861 s | 0.571–3.568 s |

## State

Purchase, reward, lost coins, exchange (two simultaneous existing-entry updates),
consumption, intent without action, refused purchase, and observed unacquired
item. Expected resulting balances and exact mutation counts were checked after
the original strict production gates, WITHOUT isolation or model repair.
All passed. This is a promising routing candidate, not proof about all inventory
semantics, long contexts or full-turn speed. Before activating the fast model,
run fresh full-pipeline cases with the now-enabled isolation and active prompt.

## Quest

Reused partial progress, memory, failed attempt, historical identity, completion,
failure, parent/child and frozen door fixtures. Five failures:
- One completion used operation=progress with completed/100; production rejected it.
- Both actual door completions quoted motion rather than the final achieved
  condition; the existing production completion gate rejected them.
- Both imagined-door attempts became terminal fail outcomes. Production gates
  accepted the proposals, but semantic fixture assertions correctly rejected them.

Keep current routing. Fix operation contract, final-condition evidence selection
and recoverable-attempt versus irreversible-failure distinction in an isolated
candidate before considering rollout. The last issue cannot be solved merely by
loosening quote matching.

## World

New recurring character, new destination, existing-character update with stable
identity/age, unchanged known entities, disproven rumor and incidental crowd.
Four failures are unambiguous missing evidenceQuote fields (new character,
new location twice, redundant known-character output during rumor case).

Read-only inspection confirmed active revision 22 has examples WITHOUT
evidenceQuote, while an appended compatibility rule requires it. This is the
same kind of prompt-contract mismatch previously found in state evaluation.
It does not establish that the fast model is intrinsically unsuitable.

The fifth strict failure is an over-constrained fixture: the new-character beat
mentions Boris owning a recurring forge. The model also created that forge,
whereas the checker required exactly one character. Creating that location is
plausibly allowed by the prompt; do NOT count this as a proven model error.
The output also supplies generic forge visual details not explicitly in the
beat, so this audit does not upgrade it to a comprehensive semantic pass.
The original 7/12 strict count is preserved, not silently rewritten.

Next: correct the isolated world JSON example and clarify the fixture's allowed
entity set before rerunning. Preserve checks for unknown identities, duplicates,
stable features, negation and absent evidence. Do not activate yet.

## Limits and verification

This is a single-model role screen, not a paired comparison to Antigravity or
an end-to-end latency benchmark. Separate prior runs showed long latency tails;
one-second medians here are not guarantees. No model repair is included.
World visual fidelity and arbitrary natural-language contradictions are not
fully verified by these structural assertions.

Added pure CheckWorldProposal wrapper and fixture regression tests: required
mutations cannot pass with empty output, negative quantities and fabricated
quotes are rejected, and valid positive controls pass. Full Go suite passed.
Only benchmark/test support was changed; running backend remains untouched.
