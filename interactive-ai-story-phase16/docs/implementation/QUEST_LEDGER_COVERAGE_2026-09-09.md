# Quest ledger coverage audit — 2026-09-09

## Implemented source classifier and live experiment

`hybridbench -quest-ledger-audit -timeline ID,ID -output FILE` now runs the
source-linked projection in a single repeatable-read, read-only transaction.
The projection and model experiment remain benchmark-only, not server settings.

Classification on the two pinned heads (329 and 24) passed snapshot comparison:

| Source category | Count |
| --- | ---: |
| Quote verified in earlier beat of same generation | 9 |
| Values consistent with reconstructed child aggregation | 20 |
| Initial/manual, no generation link | 24 |
| Unverified legacy | 48 |

Every record preserves its source event; verified quotes additionally reference
their beat, and reconstructed aggregation references the previous parent and
latest child events. Aggregation origin remains inferred, even when its numeric
rule matches exactly. A matching quote is not proof of semantic sufficiency.

Artifacts: `tools/local-llm-benchmark/results/quest-ledger-20260909-et6n4H/`.
`audit.json` is local real-data provenance; `offline.json` and `live.json` use
only synthetic stories. Real story payloads were not sent to the model.

`-quest-context-suite -quest-suite-ledger -quest-suite-live` compares full
history against full history plus the classified ledger. Two history-dependent
fixtures, two balanced repeats, eight calls, same model and prompt revision.
Inputs grow from 10377 to 11482 bytes; this tests correctness, not compression.
Both responses pass the same production evidence/domain gates and explicit gold
outcome checks. No standalone condition extraction or history deletion is enabled.

### Completed balanced model results

Eight calls completed, process exit 0, prompt revision 21,
`antigravity-preview-05-2026`. Four out of four responses passed in each variant.
Already-counted fixtures produced no changes; new-achievement fixtures produced
active parent progress 67 with a quote from the new beat. No false completion.

| Case/repeat | Full history | Full history + ledger |
| --- | ---: | ---: |
| Already counted / 1 | 17.895 s | 10.284 s |
| New achievement / 1 | 27.458 s | 49.920 s |
| Already counted / 2 | 12.248 s | 11.134 s |
| New achievement / 2 | 60.907 s | 58.376 s |

Median per call: 22.677 s baseline, 30.527 s ledger. Inputs grow 10.6 percent.
Four calls per arm do not establish a stable latency difference. No correctness
improvement or speed benefit was demonstrated by adding the ledger to full
history in these fixtures. Classification remains useful as an audit capability,
but do not enable this extra model context by default on these results.

All backend tests passed (`go test ./...`, with local HTTP listeners allowed).
The read-only audit is live PostgreSQL validation; the general PostgreSQL test
package may skip tests without DATABASE_URL and is not claimed as a full DB suite.

Read-only PostgreSQL audit; no AI calls, state writes or setting changes.
Each query batch used a repeatable-read, read-only transaction. The inspected
heads were unchanged at the end. This is not a full application replay test.

## Scope and state agreement

| Timeline | Head | Stored events | Current objectives/stages |
| --- | ---: | ---: | ---: |
| `1da4d24b-a530-420c-bc19-2280ad1a42eb` | 329 | 329, sequences 1–329 | 28 |
| `d828b450-9bea-4cbd-bd8d-f18b43898c9e` | 24 | 24, sequences 1–24 | 12 |

All 40 materialized objectives have a corresponding latest objective event.
Progress, status (after storage mapping) and success criteria agree in all cases.
These two timelines are not forks. The wider inventory includes a fork whose
local events start at sequence 3; the prototype's full-prefix requirement cannot
cover such a branch without resolving inherited events or a trusted snapshot.

## Evidence coverage

On the 329-event timeline:

- 28 creation events, 12 with nonempty evidence, 16 with generation IDs;
  4 evidence values match a contiguous quote in a beat of the same generation.
- 61 update events; all link to a beat through their generation ID.
- 41 updates contain evidence. Only 5 match the same-generation beat after
  whitespace normalization. Removing punctuation and case did not raise this count.
- 20 updates contain no evidence. All are global parents, each accompanied by
  linked child changes in the same generation. This is consistent with the current
  pipeline's automatic child-progress aggregation, which emits no evidence.
  Individual historical aggregate computations were not replayed to prove origin.

The 24-event timeline has 12 creation events with neither evidence nor generation
IDs. This is consistent with setup materialization, not missing progress history;
there are no objective update events there.

## Decision

Event provenance and current state coverage are strong for the two inspected
timelines. Semantic explanation coverage is not sufficient to replace prose.
Nonmatching evidence does not itself prove that a story outcome was wrong: it
may be legacy/paraphrased evidence and requires separate investigation.

Future ledger records need distinct provenance categories:

1. Direct evidence: event reference plus a verified quote/beat reference.
2. Derived progress: parent event plus child-event references and aggregation rule.
3. Initial/manual state: explicit source classification, not an invented achievement.
4. Unverified legacy evidence: retain source text and mark uncertainty; never
   silently upgrade it to verified evidence or discard it.

Next implementation should classify and link records before attempting any
context removal. Preserve prose when evidence is unverified or historical
conditions are not represented. Fork inheritance requires separate coverage.
