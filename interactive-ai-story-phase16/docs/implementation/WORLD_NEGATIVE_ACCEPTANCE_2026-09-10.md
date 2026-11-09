# World negative acceptance — 2026-09-10

No production routing, prompts, settings or user stories changed.

## Fresh full pipeline

Artifacts: `tools/local-llm-benchmark/results/world-acceptance-20260910-IrQgFz/`.
Four synthetic full Writer pipelines with candidate world-newbeat-quotes-v2,
Flash-Lite world/state/planner/choices, Antigravity Writer/quest, active state
prompt and journal isolation. Positive and negative groups ran concurrently,
so timings are descriptive, not controlled latency comparisons.

| Scenario | Total | World | Result |
| --- | ---: | ---: | --- |
| Boris | 43.921 s | 1.314 s | Passed; correct named blacksmith, age 40 |
| Lighthouse | 45.107 s | 0.984 s | Passed; correct named location and exterior |
| Disproven fire rumor | 34.633 s | 1.030 s | Passed; raw changes empty, no world events |
| Unchanged observation | 104.056 s | 1.010 s | Failed no-change gate |

Three of four pass full-scenario assertions. The fourth is not an established
world-evaluator false positive. Its synthetic target explicitly says the archive
is unchanged, and the scene goal says to observe an unchanged familiar place.
Planner nevertheless selects an immediateGoal to notice new details or changes.
Writer then invents a fresh facade crack, lock damage, a cut seal and suspicious
activity. World evaluation records the damage actually narrated in newBeat with
a valid quote. The first observed deviation is in Planner; causal attribution
between Planner and Writer needs an isolated test, not merely correlation.

Do not weaken the negative gate to make this run pass, and do not change the
world evaluator to ignore genuine newBeat changes. Address narrative constraint
preservation upstream. The unchanged-object fixture now has regression tests
ensuring any character/location event fails the zero-change expectation.

## Fixed-text negative world comparison

Artifact: `tools/local-llm-benchmark/results/world-negative-20260910-FrydFZ/report.json`.
Seven scenes, two repeats, balanced prompt order: 28 calls without Writer.
Baseline is the previous corrected-example candidate, NOT active production
world prompt. Candidate additionally specifies short contiguous newBeat quotes.

Six negatives: imagined damage, planned renovation, temporary colored light,
absent character, metaphorical giant, unsuccessful non-destructive entry.
Positive control: real lightning damage must update the known archive ID and
retain a description of the crack. Always returning no changes cannot pass.

- Previous candidate: 14/14; median 0.585 s, range 0.558–1.190 s.
- New guidance: 14/14; median 0.656 s, range 0.556–0.922 s.
- In each arm, negatives pass 12/12 and real-damage controls pass 2/2.
- No transport failures. Both prompts pass, so added-guidance superiority is not
  established. These explicit short negatives do not cover all indirect cases.

Tests verify negative mutations are rejected by fixture semantics, missing
positive changes fail, and actual damage passes existing production gates.
Full Go suite passed. No model or prompt was activated.

Next: freeze the failing observation context and compare Planner constraint
preservation, then replay Writer with controlled goals. Keep the world candidate
separate from this upstream failure and preserve all failed reports.
