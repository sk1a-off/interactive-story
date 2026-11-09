# Memory indexing and version publication — 2026-11-09

This increment implements the indexing worker and PostgreSQL publisher. Runtime
startup and memory retrieval remain disabled/unwired. No production stories or
provider settings were changed. Tests use synthetic data and deterministic fake
embeddings, not a real USER2 model or external service.

## Exact source reconstruction

`MemoryIndexStore.Load` validates the persisted job source and resolves trusted
lineage. It verifies the exact source snapshot, or loads the nearest snapshot
at/before the event and replays contiguous local events through that event.
Missing fork baselines, missing event history, or corrupt snapshot hashes fail
closed. It never exports today's materialized projection under an older cursor.

Documents include active facts, private character knowledge, beliefs, active
threads, chapter summaries, and established/pending world rules. Kinds and owner
scopes remain distinct. A character belief is not converted to a global fact.
Foreign branch entities are rejected instead of being relabeled as local ones.
New branch-state versions become visible at the indexing checkpoint; old source
timestamps do not make newly seeded snapshot data visible before that checkpoint.

Beat history comes directly from the immutable `beat_committed` payload. Snapshot
seed jobs do not create copies of ancestor beat history. This preserves the
ancestor visibility model and avoids publishing current beat text into the past.

## Embedding execution

The worker pins a provider/model/revision profile for its run and checks the
provider identity and 384-dimensional contract before claiming work. It embeds
only changed documents, in batches of at most 64. Document hashes include kind,
owner, and content but not the indexing checkpoint. Missing results, wrong
dimensions, non-finite values, and zero vectors are rejected.

Embedding calls happen outside the publisher's transaction. Heartbeats renew
the lease; losing it cancels the request. Processing has a timeout, bounded
retry/backoff, and generic persisted diagnostics. The worker does not separately
acknowledge a successful job: acknowledgement belongs to atomic publication.

## Version publication

Migration `00038_memory_index_publication.sql` adds typed source metadata to
`memory_embeddings`, an active-source uniqueness constraint, publication receipts,
and profile-scoped publication cursors. Legacy rows remain unclassified until
reindexed and are not silently assigned new provenance.

One transaction validates the running job's owner, token, source, and lease;
closes replaced/removed branch-state versions at E-1; writes new vectors from E;
advances the selected embedding profile's cursor; records the receipt; and marks
the job complete. The final acknowledgement also checks the database clock, so
expiry during publication rolls the transaction back.

Unchanged state versions keep their earlier intervals. Historical beat documents
do not expire just because later checkpoints omit that beat. Late old checkpoints
cannot regress the state cursor or resurrect old branch-state versions. A replay
of an already completed publication is idempotent for the same profile/sequence.

An invalid vector or lost lease rolls back both version changes and completion.
The pre-existing `memory_index_state` observed cursor is separate from publication:
future search must suppress structured memory while the selected profile trails
the observed Canon cursor. Existing search has not yet been switched to this rule.

## Executed checks

- Unit tests: separate Fact/Knowledge/Belief scopes, invalidated knowledge,
  stable hashes, foreign entity rejection, 64+1 batching, unchanged-document
  skip, invalid output (shape, NaN, zero vector), retry, and heartbeat cancellation.
- PostgreSQL: exact old event/snapshot state rather than future/current state;
  missing history rejection; closed intervals for replacement/invalidation;
  complete source-to-worker-to-vector flow with fake embeddings; publication
  rollback retaining the old active version; stale same-owner token rejection;
  idempotent receipt; historical text survival across later checkpoints.
- Migration up/down/up on `story_image_settings_00036_check`, separate from the
  application database. Down drops derived publication metadata/cursors but
  preserves embedding rows, events, and snapshots.

The standard full Go/vet/race, PostgreSQL integration, and frontend checks are
required before committing this increment. Real USER2 inference, indexing a live
library, live runtime enablement, reindex UI, and retrieval quality/latency are
not claimed by these tests.

Next: hybrid filtered retrieval using the visibility plan and profile cursors,
then Context Builder integration/fallback and disabled-by-default runtime
settings. Reindexing existing sources remains a separate explicit workflow.
