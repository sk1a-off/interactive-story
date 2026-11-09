# Vector memory: branch visibility and rollback model

Status: Stage 2 design decision, written before the new memory implementation.
The implementation status below tracks completed components. Live generation
retrieval remains disabled/unwired.

The first implementation increment now provides a domain visibility plan in
`backend/internal/domain/memory/visibility.go` and a persisted-lineage resolver
in `backend/internal/adapters/postgres/memory_visibility.go`. Unit tests cover
nested caps, safe restore, state-version boundaries, malformed lineage, and
immutable plans. PostgreSQL integration tests resolve real timeline/save-point
edges inside a rolled-back fixture transaction. Existing search and Context
Builder have not yet been switched to this plan. Migration 00037 now captures
event/snapshot work transactionally; the queue implements ordered claims,
recovery, retry, and lease-token fencing. Migration 00038 and the memory indexing
worker now implement exact-checkpoint document extraction, batched embeddings,
and atomic version publication. The worker is not started by the application
yet. The opt-in hybrid adapter now resolves this plan, filters before exact
vector/Russian lexical ranking, and suppresses branch state during profile-specific
publication lag. Migration 00039 adds Russian FTS. Context Builder integration
and runtime memory settings remain subsequent work. See
`MEMORY_HYBRID_SEARCH_2026-11-09.md` for the search checks and boundaries, and
`MEMORY_INDEX_PUBLICATION_2026-11-09.md` for publication checks.

## Existing behavior verified in this checkout

- Migration 00006 ties a child timeline to a save point in the same story. Its
  initial head equals that save point's sequence; lineage is immutable.
- `saves.Service.Fork` clones the exact saved snapshot, materializes its
  narrative projection in the child, and sets the child's head snapshot. It
  does not copy the parent's event rows into the child.
- `projection.ForkForTimeline` / `narrative.State.ForkForTimeline` assign fresh
  IDs to chapters, scenes, beats, relationships, facts, beliefs, threads, and
  instructions. Character and location identities remain shared, but their
  state is branch-local. Knowledge references are remapped to cloned facts.
- New child events continue from the saved head. A parent and child can thus
  both have event 101, with different contents.
- `saves.Service.Restore` creates a safe fork. The application does not rewind
  an existing timeline head in place.
- Migration 00008 created memory and reindex tables; 00009 changed embeddings
  to 384 dimensions. The current retriever filters one exact timeline and
  uses inclusive sequence validity. It neither follows ancestry nor filters
  embedding model/revision.
- Context Builder currently rejects any memory from another timeline. Its
  defensive filter must evolve together with the retriever, not be removed.
  The runtime context path is currently shadow-only.

## Decision: historical text and versioned state have different inheritance

Memory remains derived data. Events, snapshots, and materialized Canon remain
authoritative; retrieval cannot grant knowledge, alter resources, or override a
current fact.

Historical beat text is immutable source history. Read it from the branch where
the event was originally committed, bounded by every fork on the ancestor path.
Do not re-embed all old beat text when creating a child.

Mutable structured memories (facts, knowledge, beliefs, threads, chapter
summaries, and world rules) belong to the requested branch. Seed them from its
exact cloned snapshot, using the child's remapped IDs, then version them from
subsequent local events. Do not retrieve these rows directly from the parent's
live projection or ancestor embedding rows. This prevents a parent's later
correction from changing the child and prevents an old parent fact from
reappearing after a child edits its cloned fact.

The initial snapshot therefore requires a durable indexing job even though the
child initially has no event rows. Indexing lag may omit a useful memory; it
must never broaden visibility to compensate.

## Visibility intervals

A request carries `story_id`, `timeline_id`, owner scope, embedding identity,
and `AtEventSeq`. Validate that the timeline belongs to the story and that its
fork base <= `AtEventSeq` <= head. For a root, the base is zero. Queries before
the creation point of a child are rejected; restoring an earlier save creates
a new branch at that save instead.

Resolve an ancestor path only from database lineage, never from client-supplied
timeline IDs. For each narrative-history segment:

- local events must have sequence greater than that timeline's fork base;
- the upper bound on the requested timeline is `AtEventSeq`;
- crossing an edge to the parent caps that bound at the edge's saved sequence;
- each further ancestor uses the minimum of all caps traversed so far;
- siblings, descendants, unrelated roots, and other stories are never members;
- cycles, missing save references, mismatched stories, or malformed lineage
  cause retrieval to fail closed and use authoritative context alone.

Example (ranges are inclusive):

| Request | Visible historical event segments |
| --- | --- |
| Root A at 200 | A: 1–200 |
| B forked from A@100, queried at 150 | A: 1–100; B: 101–150 |
| C forked from B@130, queried at 140 | A: 1–100; B: 101–130; C: 131–140 |
| D restored from A@60 | A: 1–60; no A@61+, B, or C |

Structured memory for C comes from C's snapshot baseline at 130 and C's local
changes through 140. A baseline row uses `valid_from_event_seq=130` (the point
where it becomes available in C), even if its source fact was introduced much
earlier in A. Keep original source provenance separately; do not use it as C's
visibility boundary.

Existing `valid_to_event_seq` remains inclusive. A replacement or invalidation
at event E closes the old version at E-1; a new version starts at E. At E the
old state must not be retrievable. For inherited historical text, evaluate
validity at that ancestor segment's capped cursor, not the descendant's head.
Already-inactive structured entities in the snapshot do not become active
memories merely because a child was created.

## Identity, evidence, and owner scope

Every new indexed row needs a stable source key plus source version, document
class (`historical_beat` or `branch_state`), original source provenance, and
embedding provider/model/revision. Retries and reindexing cannot create multiple
visible copies of one source version under the same embedding identity.

- Timeline-scoped narrative requested for C may read allowed A/B history, but
  the stored owner of each ancestor row must be that ancestor timeline.
- Story-scoped rows still carry branch visibility; `story_id` is not permission
  to search every branch in a story.
- Character-scoped rows require the exact character identity and branch
  visibility. Unknown characters, private knowledge of another character, and
  another branch's beliefs are excluded before ranking.
- Facts, knowledge, and beliefs retain separate kinds and provenance. A belief
  is not rendered as established truth. Character dialogue context cannot gain
  knowledge just because an omniscient narrative memory mentions a fact.
- Retrieved ancestor IDs identify source evidence, not live child entities.
  They must never be used as child mutation targets.

The retriever should return original `source_timeline_id` and an explicit
resolved segment/provenance alongside the requested context scope. Context
Builder checks these against a trusted, database-resolved visibility plan.
Do not relabel arbitrary parent rows as child rows to bypass its existing check.
Unclassified legacy embedding rows remain available only to the legacy local
retriever; the new hybrid adapter excludes them entirely until safely reindexed.

## Transactional outbox and invalidation

Enqueue source work in the same transaction as the Canon event or initial
snapshot. Application observers after commit are not sufficient: a crash
between Canon commit and observer execution must not lose the indexing job.

Index exact versioned content from the immutable event or snapshot. Do not
read today's materialized row and stamp it with an old event sequence. For an
event without sufficient versioned content, reconstruct at its sequence or skip
that source with diagnostics; never silently index future state into the past.

Replacement/invalidation boundaries must be durable before asynchronous
embedding finishes. Store a branch-local source-version boundary/tombstone in
the Canon transaction. Search consults it even when an older indexing job
finishes after its invalidation. Consequently:

1. jobs can finish out of order without resurrecting a removed fact;
2. child edits close only the child's structured versions;
3. an ancestor event after the fork never enters a child's candidate set;
4. a delayed parent embedding from before the fork can become useful to a
   child, but only in its permitted historical segment;
5. snapshot indexing and later local-event indexing merge idempotently and
   cannot overwrite one another's validity boundaries.

Use bounded batches, lease recovery, attempts/backoff, and fenced completion.
Model calls happen outside database transactions. Reindex jobs build a new
revision and publish it only after completion; never mix vectors from different
embedding identities. Canon commits do not depend on the embedding service.

The initial conservative invalidation boundary is implemented by 00037's
`memory_index_state.observed_event_seq`: every committed event advances it in
the same transaction, and older snapshots cannot move it backward. Until the
index publisher reaches this cursor for the selected embedding identity,
branch-state search must omit that branch's structured memories entirely.
This sacrifices availability during indexing lag instead of risking stale
state. Per-source validity intervals are still required when publishing
versions. The cursor does not by itself mark any embedding as published.

## Retrieval and integration plan

Start with exact vector distance over a materialized, filtered candidate set.
Story-local data is small; correctness and measurable recall take priority over
using the existing global HNSW index. Check the actual query plan and corpus
size before enabling approximate search. Any future iterative HNSW path must
first verify the installed pgvector version and filtered recall.

Add a Russian `tsvector` + GIN lexical path, using the same visibility and
owner filters. Fuse vector and lexical rankings with deterministic reciprocal
rank fusion (initial k=60); deduplicate source versions before applying top-k.
This supports Russian inflections without adding another database. Evaluate
actual recall and latency before changing these choices.

Current authoritative state and recent context keep their reserved budget.
Retrieved memory has a separate bounded allowance and can be dropped entirely
on failure. It supplements director, writer, lore guard, and image prompts;
world-rule retrieval must fall back to the current complete rule selection.
Memory stays disabled by default. Reindexing and memory configuration need
explicit UI status rather than silently blocking a turn.

## Required acceptance cases

- Root/child/sibling/foreign-story isolation before ranking.
- Child and parent both write sequence 101; only the correct branch is visible.
- Nested forks clip each ancestor independently; parent future never leaks.
- Restoring an old save excludes every event after that save.
- Parent invalidates a fact after fork; child baseline remains unchanged.
- Child edits a cloned fact; ancestor state memory cannot restore its old value.
- Invalidated-at-E memory is visible at E-1 and absent at E.
- Knowledge/belief owner boundaries survive fork ID remapping.
- A pending old job completes after invalidation; the old version stays hidden.
- Snapshot indexing racing with a local mutation remains deterministic.
- Model/revision switch, reindex failure, duplicate retry, and lease loss.
- Missing lineage or embedding service yields authoritative fallback, not a
  wider search or a failed story turn.
- Hybrid RRF ordering/deduplication and memory budgets have deterministic tests.
- Measure recall@k on frozen questions and search latency independently of LLM
  response time. `cmd/hybridbench` currently measures generation experiments;
  it needs a dedicated memory mode to support these metrics honestly.

Implementation order: visibility contract and tests; transactional outbox and
version boundaries; indexing worker and snapshot seed; hybrid retrieval;
Context Builder integration/fallback; disabled-by-default settings and reindex
UI; frozen-corpus retrieval benchmarks.
