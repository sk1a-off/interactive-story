# Branch-aware hybrid memory search — 2026-11-09

This increment adds an opt-in PostgreSQL adapter. Live generation and the old
retriever remain unchanged; memory is not enabled. It is not yet connected to
Context Builder. Tests use synthetic documents and vectors, not USER2 inference
or production story data.

## Search contract

`HybridMemoryRetriever.Search` requires a finite, nonzero 384-dimensional query
vector, explicit provider/model/revision, and story/timeline/owner scope. It
resolves lineage from persisted timelines/save points. Invalid lineage, cursors,
or embedding identity produce an error for the future application fallback.
The caller will supply query text from the player action and director goal and
embed it with `EmbedQueries`; that integration is subsequent work.

Historical candidates inherit only the segments allowed by every fork boundary.
Their validity is evaluated at the source segment's capped cursor. Structured
state stays local to the requested branch, including its initial snapshot base.
Story, owner, embedding identity, branch, and inclusive validity filters all run
before either rank. Returned timeline/owner IDs identify original evidence;
ancestor rows are never relabeled as child rows.

The observed Canon cursor and selected profile's publication cursor are read
in the same SQL statement snapshot as candidates. Structured rows are omitted
unless publication has reached both the observed cursor and request checkpoint.
A different profile's cursor cannot unblock stale state. Historical text can
still be retrieved while structured indexing catches up. Unclassified legacy
rows are excluded until reindexed through the new provenance-aware publisher.

## Ranking and index choice

Migration `00039_memory_russian_search.sql` adds a stored `tsvector` using the
explicit `russian` configuration and a GIN index. This supports Russian word
forms without another service. The rollback drops only derived search data.

The initial plan materializes eligible, source-deduplicated rows before exact
cosine ranking and lexical ranking. Each list retains at most four times top-k;
RRF sums `1/(60+rank)` and uses the original UUID for deterministic ties. Top-k
defaults to 8 and is capped at 100. Empty/stop-word text yields vector-only
ranking. Overlapping source versions deduplicate by latest valid start and UUID.

Exact search is intentional for small story/branch candidate sets. It avoids
global HNSW filtering reducing recall. `EXPLAIN ANALYZE` on the synthetic fixture
confirms CTE candidate scans without the global HNSW index. The materialized
lexical scan does not use GIN in this initial small-corpus plan; the index is
available for a later measured indexed lexical path. No claim about large-corpus
performance, real-model recall, or end-to-end latency follows from this fixture.
The four-times-top-k rank window also needs evaluation on a frozen corpus.

## Validation

- Baseline full backend/frontend checks passed. Existing PostgreSQL tests passed
  before applying 00039 (new hybrid tests require its search column).
- Migration up/down/up passed on the separate
  `story_image_settings_00036_check` database; the application DB was untouched.
- Four hybrid PostgreSQL scenarios repeated ten times (40 runs) cover nested
  forks, restore, parent future, sibling/foreign story, source provenance, owner
  isolation, profile/revision filtering, invalidation lag, inclusive version
  boundaries, Russian inflections, exact RRF scores, top-k, source deduplication,
  stop words, empty text, deterministic ties, and the actual query plan.
- Input validation unit tests reject malformed scope, wrong owner, missing
  identity, zero/NaN/infinite/wrong-size vectors, and excessive query size/limit
  before any database access.
- The indexing worker integration also searches its published snapshot versions
  and confirms that invalidated state disappears.

Final checks passed: full Go formatting/vet/test/race checks, PostgreSQL suite
with race detection, and frontend install/typecheck/lint/test/build (41 tests).
The install still reports the existing 14 dependency vulnerabilities; this
increment does not upgrade frontend dependencies or claim those are resolved.
Real embeddings, live prompt integration, token budgets for inherited evidence,
reindex UI, and frozen-corpus recall/latency benchmarking remain subsequent work.
