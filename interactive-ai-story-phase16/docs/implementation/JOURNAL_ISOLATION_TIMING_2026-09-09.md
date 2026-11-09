# Journal isolation timing

Artifact directory: `tools/local-llm-benchmark/results/isolation-timing-20260909-zRbaoW/`.

## Local cost (completed)

`go test ./internal/application/generation -run '^$' -bench
BenchmarkFrozenJournalIsolation -benchmem -benchtime=200ms -count=3`.
AMD Ryzen 7 7800X3D. Frozen source texts and proposals; these are pure gate
measurements, not model calls or complete turn latencies.

| Case | Off, median | On, median | Added cost |
| --- | ---: | ---: | ---: |
| Coin update + invalid key quote | 160.871 us | 567.819 us | 406.948 us |
| Valid ration removal | 92.640 us | 178.346 us | 85.706 us |

The mechanism adds local work rather than reducing it, but less than half a
millisecond in these fixtures. The coin paths perform different useful work:
the old path rejects everything; the candidate validates a surviving balance.
No new AI call is added. This is not a claim about all future payload sizes.

## Live comparison design

Four sequential independent synthetic runs: coins off/on, ration on/off. Same
case snapshots, model routing and corrected state prompt in both arms. Only
`-journal-evidence-isolation` differs. Writer/world/quest use Antigravity;
planner/state/choices use Flash-Lite. Read-only configuration and in-memory Canon.

This isolates the mechanism on the tested candidate configuration. It does NOT
compare the entire deployed configuration against all proposed routing/prompt
changes. Each live call is stochastic, so inputs to post-Writer evaluators can
differ. Two pairs are descriptive evidence, not statistical proof of speedup.

Initial launcher failed before provider calls because systemd expanded shell
variables. The corrected launcher uses literal commands; that failed attempt
is excluded from timing results.

## Live results (all four completed)

| Scenario | Old gate | New gate | Validation |
| --- | ---: | ---: | --- |
| Spend coins | 119.704 s | 97.637 s | Both pass |
| Consume ration | 282.936 s | 41.330 s | Both pass |

All runs used active revision 21 and identical state prompt SHA-256
`01a2ecf1de9b09e3c3ca54ae194e39c65986fa752e9c1ac8210f302285fc64cd`.
Each used six model calls, zero repairs/escalations, and no transport errors.
Both candidate runs had exact state quotes: the salvage branch was not needed.
Thus these live runs do not replace the frozen error-path acceptance tests.

The numerical reduction is not attributable to isolation. Coin world evaluation
took 80.628 s off vs 67.094 s on; Writer 35.941 vs 29.395 s. Ration quest
evaluation took 254.393 s off vs 13.191 s on, dominating that pair's difference.
State evaluation was 4.769/1.415 s for coins and 0.983/1.223 s for ration
(off/on). Variance in external calls dwarfs the measured local gate overhead.

Conclusion: the new mechanism adds a small local cost; no causal full-turn
speedup is established. It is a data-preservation experiment, not a latency
optimization. To reduce perceived wait, address world/quest long-tail latency
and early text delivery separately. No working settings were activated.
