# Branch-aware Context Builder — 2026-11-09

This increment connects the hybrid retrieval contract to Context Builder. It
does not enable memory in live generation or start the indexing worker. The
existing context-shadow runtime still uses the legacy local-only retriever.
There are no new migrations, external dependencies, or provider-setting changes.

## Trusted evidence

The opt-in builder receives a persisted-lineage resolver and a pinned embedding
provider/model/revision. It verifies that the resolver's immutable visibility
plan matches the requested story, branch, and event checkpoint. It checks the
query provider's identity, embedding kind, and 384 dimensions before model work.
Only `EmbedQueries` is used, with the same text sent to Russian lexical search.

The hybrid adapter now returns the actual stored embedding identity alongside
the original source timeline/owner, document class, source key, and version
interval. Context Builder independently filters those fields before sorting:

- inherited historical evidence must lie in an allowed ancestor segment and
  be valid at that segment's cap, rather than at the descendant's later cursor;
- structured state must belong to the requested branch and its lifetime;
- character evidence requires the exact owner; ancestor structured knowledge
  cannot be substituted for the requested character's local state;
- wrong model/revision, missing provenance, unknown document classes, non-finite
  scores, sibling/foreign scope, and invalid intervals are excluded.

Rows retain their original IDs and kinds. Source, ID, and normalized-content
deduplication precede the result limit. RRF score ties retain UUID order. The
builder copies candidates before sorting instead of mutating adapter-owned rows.
Publication-lag fencing remains the SQL adapter's responsibility; the separate
lineage resolver does not attest that a branch has finished indexing.

## Budget and failures

Branch-aware retrieval has a separate default allowance of 1024 estimated tokens.
`Input.MemoryTokenBudget` can override it; zero/negative skips model/database
work. The legacy builder keeps its old budget when no override is supplied.
Current authoritative state, recent beats, and summaries keep their existing
priority. Retrieved evidence uses only the remaining overall allowance and the
separate memory allowance, including serialized source/profile metadata. The
counter is the existing deterministic rune-based estimate, not a model tokenizer
or a guarantee about a provider's exact input-token count. A context already
exhausted by authoritative state skips retrieval entirely.

A shared two-second deadline covers lineage, query embedding, and search by
default for the branch-aware path. It can be shortened/overridden by the caller,
and parent cancellation is preserved. Dependencies must honor context
cancellation; the builder does not launch abandoned background model calls.
Visibility/embedding/search failures return the budgeted authoritative fallback
and a generic `RetrievalError` for the calling layer's diagnostics. Raw database
or provider errors are not propagated into that diagnostic. Live logging and
role-specific prompt assembly are subsequent integration work.

## Checks

- Baseline full backend/frontend and PostgreSQL/race checks passed.
- Unit coverage: adversarial retriever rows (foreign/sibling/future/expired
  sources, parent state, relabeled owners, model/revision mismatch, missing
  provenance, NaN/infinite scores), character isolation, independent resolver
  failure/mismatched plan, malformed query embeddings, provider/search failures,
  deadline cancellation, zero/exhausted budget skipping dependencies, separate
  memory cap including metadata, top-k, deduplication, and stable hybrid ties.
- Real PostgreSQL hybrid search plus the branch-aware builder, using a fake
  384-dimensional query provider, passes with nested lineage and original source
  IDs. The four hybrid scenarios were repeated ten times. Existing snapshot
  publication/search and all older regression tests remain required.

Final checks passed: full formatting/vet/Go tests/race with PostgreSQL and
frontend install/typecheck/lint/test/build (41 frontend tests). The install still
reports the existing 14 dependency vulnerabilities; no dependencies changed.
These are correctness checks, not a claim about real USER2 recall or generation
latency.

Next: opt-in role-specific memory assembly for director/writer/lore guard/image
prompts, world-rule fallback, runtime settings, worker startup, and reindex UI.
Memory remains disabled until those paths and their failure behavior are tested.
