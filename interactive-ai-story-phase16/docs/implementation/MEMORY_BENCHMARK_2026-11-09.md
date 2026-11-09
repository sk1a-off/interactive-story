# Frozen memory retrieval benchmark — 2026-11-09

The new `cmd/hybridbench -memory-bench` mode measures the existing retrieval and
supplement code without invoking a story LLM. Live USER2 inference was run after
preparing a temporary CPU service. Production AI settings and story data were
not changed.

## Dataset and method

The versioned fixture is
`backend/cmd/hybridbench/fixtures/memory-retrieval-v1.json`: 34 synthetic Russian
documents, 12 scopes, and 39 questions. Thirty-five questions have manually
annotated primary source IDs; four test empty or invalid scopes. Documents and
questions were authored before the live USER2 measurement. The corpus hash is
`79feecb67b064cd62ab8fae4a37c1dfc981f4c7bb22cde245c34ef59629689bf`.

The fixed cases include parent/child/nested branches, safe restore, future and
expired versions, replaced state, index lag, private character owners, foreign
stories, other embedding identities, unclassified legacy rows, and excluded
world-rule memories. Allowed source IDs are independently annotated rather than
derived from the retrieval SQL. One question requires two source documents.

Document embeddings are created in batches of at most 64 and query embeddings
one question per request. All external inference completes before a PostgreSQL
fixture transaction begins. Fixture data is rolled back, including on failure.
This requires an explicitly selected isolated database: the benchmark never
defaults to the normal story DSN. The transaction uses outbox/lineage locks, so
an isolated database is also necessary to avoid disturbing application traffic.

There is one unmeasured warmup per question/k pair. Each measured run repeats
search 20 times at k=1/3/5/8 with seeded shuffled order: 2960 valid-scope searches
and 160 expected-rejection searches per run. Rejected-scope latency is separate.
Median and nearest-rank p95 are reported overall and by k. Search time includes
lineage resolution and exact filtered RRF ranking; query embedding is measured
separately. It is not a measurement of story-generation latency.

Recall is macro-averaged once per unique annotated question and k. Repeated
searches do not inflate the semantic sample size. Errors retain zero recall;
only the expected domain visibility error satisfies an invalid-scope case.
Duplicate/unexpected sources, excessive result counts, query errors, and unstable
results fail the correctness gate. A completed real-model run is labeled
`measured`, not a quality-approval verdict. Fake runs are labeled `smoke_only`.

## Live USER2 results

Two independent full runs regenerated embeddings and used different search-order
seeds. Retrieved source IDs, recall, and budget replay results matched exactly.

| k | Macro recall over 35 questions |
| --- | ---: |
| 1 | 94.29% |
| 3 | 98.57% |
| 5 | 100% |
| 8 | 100% |

The two-source question cannot reach 100% recall at k=1 by definition. At k=3,
`two-evidence-route` returned `child-summary`, `bridge`, and `child-gate`, missing
the annotated `market` source. Both required sources were retrieved by k=5.
Primary-source labels are not an exhaustive ranking of every useful supporting
summary, so a missing label is not automatically evidence of bad generated prose.

| Measurement | Run 1 median / p95 | Run 2 median / p95 |
| --- | ---: | ---: |
| Query embedding, 39 requests | 10.42 / 14.28 ms | 11.44 / 13.89 ms |
| Valid-scope search, 2960 samples | 0.524 / 0.803 ms | 0.573 / 0.856 ms |

Both runs had zero annotated-scope violations, search failures, and unstable
search outputs. Do not add the independent medians to claim end-to-end latency.
These are small-corpus, warm-cache, single-client measurements without exclusive
control of other host activity or a concurrent indexing load.

## Memory that reaches the prompt budget

The benchmark additionally replays the actual narrative `memorycontext.Service`
and Context Builder at top-k=8 using cached real query vectors. This measures
the production serialized policy/provenance budget without external inference
inside the fixture transaction. The 33 narrative questions exclude private
character scopes and safety negatives. Current state is kept separately; recent
beat deduplication is not simulated.

| Estimated token allowance | Macro recall after budget | Mean retained documents |
| --- | ---: | ---: |
| 512 | 98.48% | 2.18 |
| 1024 | 100% | 5.88 |
| 2048 | 100% | 7.64 |

At 512 the two-source question retained the bridge and a supporting summary but
dropped the annotated market document. The current defaults, k=8 and 1024
estimated tokens, are retained. This corpus does not establish that k=5 is a
safe replacement across real stories or that a larger budget is never useful.

## Environment and limitations

The recorded environment is AMD Ryzen 7 7800X3D, CPU inference with configured
OMP/MKL thread limits of 2, PostgreSQL 16.15, pgvector 0.8.6, and exact filtered
search. The local model cache points to USER2 snapshot
`23f65b34cf7632032061f5cc66c14714e6d4cee4`. Full package versions and model metadata
are in `benchmarks/memory-user2-environment-2026-11-09.json`.

The project pins sentence-transformers 3.4.1; the downloaded model metadata
names 4.0.2 and emitted a compatibility warning. Results describe the installed
project version with transformers 4.57.6 / torch 2.15.0+cpu. The service currently
uses `encode` without named task prompts, matching the existing implementation.
No project dependencies or production embedding behavior were changed.

This is a regression baseline on hand-authored, correlated questions, not a
statistically representative evaluation of real stories. Repetition improves
timing observations, not semantic diversity. Large histories, concurrent traffic,
actual recent-context deduplication, story coherence, and complete turn latency
remain unmeasured. Useful next experiments are a larger independently reviewed
corpus, the model/library compatibility warning, and model-task prompting after
checking the model's documented contract.

## Artifacts and reproduction

- `benchmarks/memory-user2-baseline-2026-11-09.json`: live measurements and rows.
- `benchmarks/memory-user2-repeat-2026-11-09.json`: independent repeat.
- `benchmarks/memory-user2-environment-2026-11-09.json`: model snapshot/packages.
- `benchmarks/memory-fake-smoke-2026-11-09.json`: mechanical control only; its
  recall is not semantic evidence.

With a local USER2 service already running, from `backend`:

```bash
GOCACHE=/tmp/interactive-story-go-cache go run ./cmd/hybridbench \
  -memory-bench \
  -memory-database-url 'postgres://story:story@127.0.0.1:5432/story_image_settings_00036_check?sslmode=disable' \
  -memory-provider user2 -memory-repeats 20 -memory-seed 20261109 \
  -output /tmp/memory-user2-report.json
```

The isolated database must already have migrations through 00040. An optional
`-memory-corpus` accepts a revisioned JSON file. `-memory-provider fake` explicitly
selects a dependency-free mechanical smoke. USER2 health must report the expected
model and 384 dimensions; invalid vectors or service failure produce a failed
report instead of silently substituting a fake provider.

The temporary service for this run used the existing requirements in
`/tmp/story-memory-bench-venv` and cached weights under
`/tmp/story-memory-bench-hf`. Its startup command from the project root was:

```bash
HF_HOME=/tmp/story-memory-bench-hf OMP_NUM_THREADS=2 MKL_NUM_THREADS=2 \
TOKENIZERS_PARALLELISM=false /tmp/story-memory-bench-venv/bin/uvicorn \
  tools.embedding_provider.app:app --host 127.0.0.1 --port 8090
```

The temporary benchmark service was stopped after measurement; this is not an
installed persistent service. Baseline and final full Go format/vet/tests/race
with PostgreSQL, five embedding-provider Python contracts, and frontend
install/typecheck/lint/test/build passed (45 frontend tests). The existing 14
frontend audit findings remain; no project dependencies changed.
