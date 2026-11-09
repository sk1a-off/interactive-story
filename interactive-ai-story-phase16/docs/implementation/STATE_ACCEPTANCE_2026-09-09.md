# State-only candidate acceptance — 2026-09-09

Artifacts: `tools/local-llm-benchmark/results/state-acceptance-20260909-sczJw2/`.
Production configuration, prompt revision and Canon were not changed.

## Negative replay

Three synthetic scenes, two repeats, balanced baseline/candidate order (12 model
calls). Both baseline and short-quote candidate passed 6/6: every raw response
was an empty changes array. Cases: spending intent without payment, refused
purchase with neither payment nor transfer, and an observed key not acquired.
Candidate latency 0.981–21.274 seconds. These are isolated fixed-text tests, not
full Writer runs or proof about all negative situations.

## Full pipeline

Two fresh synthetic runs using `-fixture-only`, state-shortquote-v1.md and
Flash-Lite for state. Writer, world and quest remained Antigravity with their
active prompts. Planner/choices retained Flash-Lite. The new `-state-prompt-file`
override is in memory only; report records its absolute path and SHA-256.

- Spend 3 of 10 coins: failed acceptance, total 88.015 seconds. State 1.253 s.
  The proposed coin balance of 7 and its evidence were correct. A second change
  created a received key, but quoted `со стойкой со звоном лег...` rather than
  the actual `на засаленную стойку со звоном лег...`. Production evidence
  validation rejected the entire batch, so no journal events were produced.
  Writer did explicitly narrate taking the key later; this was a quote fidelity
  error, not proof of non-acquisition.
- Consume final ration: passed every required event/state assertion, total
  45.783 seconds. State 1.080 s. Journal event set quantity 0, status inactive.

No transport errors, repairs or escalations occurred. Other measured role maxima:
Writer 24.572 s, quest 37.336 s, world 33.937 s, choices 26.114 s. These roles
overlap in part; do not sum their durations. Only two runs, no paired timing
baseline: this is not proof of an end-to-end speedup. Full pipeline passed 1/2.

## Decision

Do not activate the fast state candidate yet. Prompt-only changes improve
contract compliance but do not eliminate malformed quotes. A single invalid
secondary change can currently discard a valid money update while prose commits.
Next investigate local, dependency-aware per-change validation or narrowly scoped
repair, with explicit tests for conflicting/duplicate IDs and transactional
dependencies. Do not simply accept approximate quotes or weaken evidence gates.
Prefer an approach without an additional model call on the critical path.

Added tests for negative assertions, state-only prompt isolation, and the
observed all-or-nothing batch failure (valid expense alone passes). Full Go suite
passed before the final test-only additions; targeted tests passed afterwards.
No production semantic changes or live database integration acceptance claimed.
