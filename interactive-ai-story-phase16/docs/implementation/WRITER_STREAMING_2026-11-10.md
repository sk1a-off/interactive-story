# Stage 3.6 — optional writer streaming

## Behavior and rollout

AI Settings now offers “Показывать текст во время генерации”, persisted in the
immutable AI revision as `storyLlmStreamWriter`. Its legacy/default value is
false. No active revision was changed. Queued jobs keep their pinned revision.
The existing completed-writer provisional preview remains unchanged when this
option is off. Story setup/editing and non-writer calls are not streamed.

When enabled, the Reader shows an explicitly provisional cumulative draft while
the writer is still responding. The completed, validated candidate replaces it.
A semantic rewrite retracts the previous candidate and replaces it with the
rewrite; a failed/cancelled/retrying attempt clears the draft. Canon, saves,
branches and choices still use the final validated text and one atomic event
batch. Reading partial text does not enable the next action early.

No migration, dependency, model-route change or prompt rewrite is required.
The setting itself is not enabled automatically by this increment.

## Provider handling

The existing adapter opts into streaming only for writer requests carrying an
`OnText` callback. Callbacks are never serialized into request JSON. The router
and telemetry wrapper forward the same request, so Safe Hybrid keeps the writer
on its configured quality model. Other providers/adapters that ignore callbacks
continue to return a complete reply.

Chat completions request `stream:true` with final usage and accumulate only
`choices[0].delta.content`. A normal finish and `[DONE]` are required. Interactions
requests `stream:true` and reads only text deltas from steps identified as
`model_output`; thought, signature, tool and other internal steps are ignored.
An `interaction.completed` event with completed status is required. Final usage,
cache counters and provenance remain attached to the one logical response.

The event contracts were checked against the official
[Gemini streaming guide](https://ai.google.dev/gemini-api/docs/streaming) and
[compatibility guide](https://ai.google.dev/gemini-api/docs/openai).

An incremental JSON scanner extracts only the top-level `text` string, including
split UTF-8, JSON escapes and surrogate pairs. Nested text/private fields and
JSON syntax are never shown. Final JSON is still normalized and validated;
truncation, invalid text, duplicate output keys or missing completion markers
fail the attempt. Writer JSON is bounded at 1 MiB and SSE input at 4 MiB.

Only explicit unsupported-stream HTTP 400/422 permits one full-response fallback.
Unsupported schema negotiation remains separately bounded. Endpoints that ignore
the stream flag but return a valid complete response are accepted without a
second generation request. Capability rejection is cached per client instance.
Once a stream has started, an error never silently concatenates a replacement
request or rotates credentials into that draft. Cancellation stops the request.
Existing durable retry policy owns the next attempt. Protocol retries, effective
streaming and fallback are reported separately from structured repair.

## Storage, replay and ordering

`draft_streaming` updates carry **cumulative text snapshots**, despite the legacy
`textDelta` field name. Updates are throttled to roughly 75 ms after the first
text. This avoids missing characters when the bounded live hub drops frames for
a slow reader. The browser replaces a snapshot rather than appending it.

`PersistedSink` bypasses SQL for streaming frames, including empty retraction
frames. The hub retains only the latest snapshot per generation. Replay merges
ordinary durable updates with that current process-local snapshot. Finished,
failed and retrying generations clear it. Complete candidate/final updates use
the existing persistence path; there is no row per token and no partial Canon.

Transient events have sequence zero and are sent **without an SSE id**, preserving
`Last-Event-ID` for durable updates. Connection-local revision guards deduplicate
replayed snapshots and reject late frames after a final draft or retry barrier.
A streaming attempt uses its own million-wide revision range; the next attempt
cannot be overwritten by an older queued frame. The Reader applies the same
barrier and accepts explicit empty resets. Legacy non-streaming revisions keep
their existing behavior.

Transient fragments do not survive a server process restart. The durable job and
ordinary updates still do; its next attempt resets/replaces the draft, and only
completed Canon is authoritative. Browser reload/reconnect while the process is
alive gets the latest available snapshot. This is not persistent token history.

The existing persisted `writerReadyMs` timing retains completed-writer semantics.
An additional log-only `first_stream_text_ms` records early availability without
changing that API field or adding a metric column.

## Live compatibility smoke

One frozen conversation pair used the real configured Antigravity writer with
scripted planning/evaluators/choices and in-memory Canon capture. Both arms
succeeded with four choices, and both writer requests returned HTTP 200. The
stream arm used native streaming with no protocol fallback and produced 12
non-empty draft updates.

| Mode | First available text | Completed writer |
| --- | --- | --- |
| Buffered | 33.39 s | 33.39 s |
| Streamed | 36.48 s | 39.61 s |

The stream exposed text **3.13 s before its own completion**. Its first text was
still later than the buffered reply in this single pair. This proves the
incremental delivery mechanism, not a faster model or a reliable before/after
latency improvement. Agent startup/reasoning before the first model-output
fragment remains part of the wait. Prose differs between calls and has not had
blind literary review. No full provider chain, browser rendering or durable SQL
commit latency is inferred from this smoke.

[Live pipeline smoke](benchmarks/writer-stream-live-2026-11-10.json),
[derived availability analysis](benchmarks/writer-stream-analysis-2026-11-10.json),
[fake mechanical smoke](benchmarks/writer-stream-fake-2026-11-10.json).
Original reports are copied byte-for-byte. Final refinements to cancellation,
replay guards and log-only timing after the live probe do not alter successful
provider payloads or the observed deltas. Full regression checks cover them.

## Verification and reproduction

Tests cover byte-by-byte Unicode/escape decoding, private/nested-field exclusion,
agent thought/tool filtering, usage retention, prefix-before-completion,
truncation, duplicate/oversized output, cancellation, bounded stream/schema
fallback, default off, semantic rewrite/retraction, Canon final-text-only,
SQL-frame exclusion, latest-snapshot replay, retry barriers and transient SSE ids.
UI tests cover setting save/restore, cumulative replacement, reset, retry and
late-frame rejection. The existing coherence guard keeps choices locked until
new Canon and its matching choice set are visible.

From `backend`:

```bash
go run ./cmd/llmbench -writer-stream-bench -turn-provider configured \
  -turn-config-database-url 'postgres://story:story@127.0.0.1:5432/story?sslmode=disable' \
  -turn-fast-state -turn-repeats 1 -output /tmp/writer-stream-live.json
```

Use `-turn-provider fake` without a configuration database for mechanical checks.
The smoke reads metadata only and writes no story/queue/index fixtures. Local
llama.cpp streaming is protocol-tested; an actual local model and a manual live
browser were not exercised. Rejection fallback uses controlled HTTP tests; the
selected live Antigravity endpoint supported the stream directly.

Before and after checks passed: Go formatting, vet, all backend tests and the
full race suite with isolated PostgreSQL, frontend dependency install/typecheck/
lint, all 53 frontend tests and production build. The earlier suite had 48 tests;
three setting/save tests and two reader snapshot tests account for the increase.
The targeted rewrite/persistence/epoch suite passed ten race-detector repetitions,
and the earlier 24-output live schema replay still passes. The dependency audit
still reports the same 14 pre-existing findings.

## Next point

Stage 3.7: request limits, jittered backoff, bounded concurrency and giving player
turns priority over illustration-prompt work. Previous 503/429 measurements make
provider availability important independently of streaming.
