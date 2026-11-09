# Memory indexing outbox — 2026-11-09

This increment implements durable work capture and queue reliability. It does
not yet execute embedding calls or feed memory into generation.

Migration `00037_memory_index_outbox.sql` creates `memory_index_jobs` and
`memory_index_state`. After-insert triggers capture story events and snapshots
in their own Canon transaction. A rollback leaves no job behind. Source IDs,
branch/story scope, sequence, and creation time are immutable. Unique source
references make repeated enqueueing idempotent.

Snapshot capture includes a new fork's cloned baseline even when the child
has no local event rows. Jobs reference exact source events/snapshots; they
must not later read current materialized rows and stamp them with old cursors.
Existing pre-migration events and snapshots are not backfilled automatically;
the later reindex workflow must cover them.

`memory_index_state` records the highest observed committed source sequence.
An old snapshot cannot reduce it. The later retrieval path must suppress
structured branch memory while publication trails this value. Historical
text still requires the ancestor visibility plan. No retrieval code uses
this marker yet, and no index publication is claimed by this increment.

The PostgreSQL queue provides:

- one running job per branch, including when an old snapshot arrives late;
- source sequence ordering within each branch;
- cross-branch work eligibility while another branch is waiting;
- positive-TTL leases, heartbeat, delayed retry, and bounded attempts;
- a fresh random lease token on each claim, even for the same worker name;
- rejection of completion, heartbeat, or retry from an old token or lease;
- expired-lease recovery on each claim, persisted even when no job can run;
- an optional story scope for a dedicated worker or future reindex run.

An exhausted job blocks later jobs in its branch until repaired/reindexed;
silently skipping it could publish an incomplete state history. Other branches
remain eligible. Search must use authoritative fallback during this lag.
Publication of embeddings and successful queue completion will need one fenced
transaction in the indexing worker; calling `Complete` independently is not an
embedding publication protocol.

PostgreSQL integration tests exercise atomic enqueue/rollback, duplicate
sources, immutable provenance, monotonic change cursors, serial branch work,
late snapshots, backoff, recovery, same-owner stale tokens, exhausted attempts,
and persistence of terminal recovery when claim returns no job. Fixtures roll
back in their own transactions. These tests do not simulate an embedding
provider or claim a completed vector-memory pipeline.

Migration up/down/up was tested on the separate
`story_image_settings_00036_check` database. Down removes only the derived queue,
its cursor table, and their triggers/functions. It does not remove Canon events,
snapshots, or existing `memory_embeddings` rows. Downgrading discards queued
indexing work, which must be rebuilt on a later upgrade.

Next increment: reconstruct exact source checkpoints, build typed/versioned
documents, call `EmbedDocuments` outside the transaction, and publish vectors
and inclusive validity intervals atomically under the current lease token.
