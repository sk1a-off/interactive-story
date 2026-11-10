# Stage 3.5 — prefix-cache measurement

## Decision

Retain runtime request ordering. The adapter already places role instructions
before changing user input. An experimental reorder inside user JSON increases
the shared synthetic prefix, but the live experiment provides no evidence of
additional cache hits or a cache-caused speed benefit. The ordering helper stays
inside `cmd/llmbench`; production `req.Input`, prompts, model routing, reasoning,
AI revisions and launcher switches are unchanged.

The permanent addition is optional provider cache telemetry in responses,
per-role logs and offline traces. No cache manager, explicit cache object,
conversation state, cache-control parameter, UI setting or migration is added.
Local llama.cpp request behavior remains unchanged.

## Protocol evidence and limits

The [Google caching guide](https://ai.google.dev/gemini-api/docs/caching) documents
automatic implicit caching and recommends common content before changing input.
Its current table does not state a minimum specifically for the selected Gemini
3.5 Flash-Lite or managed Antigravity model. Interactions supports implicit
caching; switching API or managing explicit caches is outside this experiment.
No threshold was invented and the synthetic contexts were not padded.

The adapter reads optional `usage.prompt_tokens_details.cached_tokens` in chat
compatibility replies and documented
[`usage.total_cached_tokens` in Interactions](https://ai.google.dev/api/interactions-api).
An absent, null, malformed or negative count remains unknown; a valid zero stays
an explicit zero. Neither invalid telemetry nor a new unknown usage field may
reject otherwise valid story output. Cached tokens are a reported input subset,
not additional tokens to sum into total input or a calculated monetary saving.

No cache counter was exposed in the parsed compatibility field for the planner.
This does not prove the service did no implicit caching; provider-specific
metadata outside the supported fields was not archived. The writer's actual
Interactions field was present and zero for every successful measured reply.
Managed-agent cache accounting can also include internal agent work, so even a
positive counter would need interpretation before attribution to story JSON.

## Method and controls

Six frozen synthetic requests cover planner/writer × conversation/inspection/
danger. Each variant changes the player action or intent, previous beat, and beat
counters; story bible, cast and world remain constant. These are small, natural
fixtures derived from the existing pipeline corpus, not a representative sample
of long user stories.

Two repeats × two layouts × six cases produce **24 measured requests**. Layout
order reverses per variant on the second repeat. Both arms use the same parsed
JSON data (asserted by canonical semantic SHA-256), prompt revision, model,
JSON-object output and existing sampling/reasoning defaults. Only JSON member
order differs. Unknown fields, null/empty values and numeric precision are
preserved. Ambiguous duplicate top-level/context keys are rejected.

`static_first` orders the immutable bible/player/cast/world part of context
before recent beats and counters, then moves action/new text toward the end of
user JSON. The main server never invokes this helper. Shared-prefix lengths are
UTF-8 **bytes**, not tokenizer counts or proof of minimum cache eligibility.

One request runs at a time. A successful credential probe selects and pins one
credential per role for both layouts. The two preflight replies are retained
separately and excluded from the 24-request comparison. No measured request
rotates credentials or escalates to another model. The source records an opaque
slot number, never the credential. No private user-story content is read.

Implicit caches cannot be cleared or made reliably cold here. Preflight can
already warm the current layout, and the two arms share system instructions.
Exact repeated inputs are explicitly marked, separately from novel variants;
this limits inference about reuse of a prefix across genuinely new turns.

## Results

The measured phase returned **23/24** valid replies. One static-first planner
request ended with HTTP 503; it was preserved and not replaced. With preflight,
26 physical POSTs returned 25 status 200 and one status 503. No GET polling,
transport failure, credential rotation, model escalation or structured repair
occurred. `acceptancePassed=false` records the unavailable request, rather than
claiming a clean acceptance run.

| Role | Layout | Valid replies | Shared user prefix, bytes | Cache evidence on valid replies | All-call median, s |
| --- | --- | --- | --- | --- | --- |
| `turn_planner` | `current` | 6/6 | 12 | 6 unavailable | 1.12 |
| `turn_planner` | `static_first` | 5/6 | 1603 | 5 unavailable | 1.36 |
| `writer` | `current` | 6/6 | 31 | 6 reported zero | 26.58 |
| `writer` | `static_first` | 6/6 | 1603 | 6 reported zero | 26.25 |

All twelve measured writer replies reported zero cached tokens. Cache usage was
unknown for all eleven successful planner replies. Prefix geometry improved
from 12/31 bytes to 1,603 bytes, but that alone demonstrates no cache benefit.

There are five complete successful planner pairs and six writer pairs. Median
`static_first minus current` was **+420.31 ms** for planner and **+568.38 ms** for
writer. The writer's unpaired medians happen to suggest a small opposite
ordering; they must not be treated as a causal comparison. These small, noisy,
cache-uncontrolled samples justify neither a speed improvement nor a general
claim that larger contexts cannot cache. Shape checks also do not approve
literary equivalence after reordering.

Evidence:
[raw comparison and preflight](benchmarks/prefix-cache-comparison-2026-11-10.json),
[derived analysis](benchmarks/prefix-cache-analysis-2026-11-10.json),
[fake smoke](benchmarks/prefix-cache-fake-2026-11-10.json).
Original reports are preserved byte-for-byte; the analysis records the source
hash. Fake results are labeled `smoke_only` and provide mechanical evidence only.

## Reproduction

From `backend`:

```bash
go run ./cmd/llmbench -cache-bench -turn-provider configured \
  -turn-config-database-url 'postgres://story:story@127.0.0.1:5432/story?sslmode=disable' \
  -turn-repeats 2 -output /tmp/prefix-cache-comparison.json
```

Use `-turn-cases turn_planner` or `writer` to select a role, or a case such as
`writer/conversation`. One selected case cannot demonstrate common-prefix reuse
across different variants. Use `-turn-provider fake` without a database for a
mechanical smoke. The configured path pins active metadata read-only and never
writes story events, index fixtures, API settings or provider cache objects.
Reports checkpoint after each call; unavailable roles and interruptions remain
explicit. A completed measurement can fail acceptance.

## Validation and environment recovery

At task start, the existing project PostgreSQL container was stopped. The first
integration baseline and metadata lookup failed with connection refused. Only
`deploy-postgres-1` was started; its existing volume, production migration level
and AI revisions were preserved. It remains available for project development.
No other application/database container was started.

After database recovery, the original `2e2c7be` backend race suite passed from an
isolated temporary source archive, and the working checkout's database suite
passed too. The temporary source copy was removed; database tests used the
existing isolated `story_image_settings_00036_check` database.

New tests cover both usage protocols, zero versus missing/invalid usage,
unchanged runtime input and system-first messages, semantic JSON equivalence,
large-number precision, unknown/null/empty values, deterministic ordering,
duplicate/malformed object rejection, shared-prefix geometry, case filtering,
exact-repeat labeling, and failure/cache accounting.

Final Go formatting, vet, all tests and the full race suite with isolated
PostgreSQL passed. Frontend dependency install, typecheck, lint, all 48 tests
and production build passed; the dependency audit still reports the same 14
pre-existing findings. The earlier 24-output schema validator replay passed too.
These engineering checks do not turn the one unavailable provider reply into
a clean live acceptance run. No browser, full turn, real SQL Canon commit or
local model was exercised in this cache experiment.

## Next point

Stage 3.6: optional writer streaming through the existing generation updates,
with completed-text persistence, replay and draft replacement preserved.
