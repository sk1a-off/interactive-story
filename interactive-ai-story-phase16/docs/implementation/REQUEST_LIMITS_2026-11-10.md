# Story LLM request limits — 2026-11-10

## Decision and scope

Stage 3.7 adds a shared, process-local admission controller and bounded HTTP 429
recovery. The earlier reasoning experiment recorded 47 physical 429 responses
and 30 terminal 503 failures. This change addresses quota retry bursts and
competition for slots; it does not establish faster model inference or fix 503.

The server injects one `RequestControl` into every client created for AI/prompt
revisions, setup, Director, generation and image prompts, including both
safe-hybrid models. A controller confined to an individual client would not cap
aggregate runtime traffic because the factories create clients repeatedly.

Admission pools use provider plus URL origin, excluding credentials and model.
Google chat and Interactions therefore share capacity and short cooldown even when
their URL paths or models differ. Different origins are independent. This is
conservative: keys/projects are not mapped to independently measured quota
budgets. Multiple keys do not create additional admission capacity. Multiple
backend processes need an external gate to enforce a deployment-wide limit.

## Admission and priority

- Maximum three physical Google requests per origin across all clients/models.
- Maximum one local OpenAI-compatible request per origin; the existing local
  sequential pipeline policy also remains in place.
- A successful SSE response retains its slot until its body is closed. A normal
  response retains it through body reading/closing. Errors release the slot.
- Requests queue FIFO within foreground and background classes. Waiting
  foreground work precedes waiting image roles: `image_prompt`, `image_moments`,
  `image_next_moment`, `image_paragraph`.
- Active calls are not preempted. Priority applies when admitting an HTTP role
  request, rather than reserving all capacity for an entire turn between roles.
  Continuous foreground pressure can postpone image preparation.
- Cancellation/deadline removes a queued request without spending a POST or
  leaking capacity. Cooldown waiting holds no physical request slot.

The cap of three preserves the existing world/state/quest fan-out for a single
Google turn. Under contention, simultaneous turns may wait for capacity.
Uncontended successful requests have no added sleep. The Perchance browser
worker, embedding service, Canon, event ordering and saved AI revisions are
outside this transport gate.

## Retry policy

HTTP 429 and Google's embedded 429 envelope in a successful HTTP response use
the same policy in chat, Interactions and pre-acceptance stream requests:

1. Read and close the rejected response, then install a shared cooldown before
   releasing capacity so queued requests cannot bypass the observed rejection.
2. Use equal jitter: retry delays are 0.5–1s, 1–2s and 2–4s. A final exhausted
   rejection also sets a 4–8s cooldown for subsequent requests.
3. Honor `Retry-After` seconds or HTTP dates for the rejected credential/model
   (bounded to 24h). Hints up to 30s also extend the short shared cooldown. Longer
   hints skip only that credential/model, allowing other configured Google keys
   and other models to proceed after exponential backoff. If no identity is
   available, fail without holding a job asleep until a distant quota reset.
4. Google requests advance the existing key cursor; other providers retain
   their configured identity. No immediate rotation burst bypasses cooldown.
5. Allow at most three quota retries per `Generate` call. Schema/unsupported
   stream fallback retains the same retry budget and priority. Auth-key skips
   remain separately bounded by the configured key count. An existing
   safe-hybrid escalation or durable job retry is a separate higher-level attempt.

Rejected Google keys (explicit 401/403 key/permission markers) are skipped
without quota backoff and are visited at most once in a transport attempt.
Ordinary 5xx, invalid JSON, unrelated 4xx and transport failures do not acquire
new automatic retries. Raw provider bodies, headers and transport error strings
are not included in errors. Accepted partial streams are never restarted or
concatenated with another key; truncation still fails through existing handling.

## Verification

Controlled tests exercise:

- exponential/jitter bounds, Retry-After seconds/date/invalid/past/long values;
- bounded quota exhaustion for local chat, Google chat/Interactions/streaming,
  and embedded Google errors, with sanitized errors and expected key rotation;
- shared limits across 24 independently instantiated clients/models: observed
  peak three hosted calls and one local call;
- an earlier queued illustration yielding to a later player request;
- shared cooldown across models, with the player overtaking an image retry and
  no occupied HTTP slot during the wait;
- cancellation in both capacity and quota waits, independent origins, repeated
  release, HTTP/body errors, and stream-body lifetime;
- quota recovery before a stream, truncation without a new POST, and quota
  budgets surviving schema and unsupported-stream fallback;
- a real local HTTP server returning 429 + `Retry-After: 1`, followed by success:
  exactly two POSTs, identical generation payload, and at least one second wait.

The queue/quota scenarios passed ten race-detector repetitions. The full adapter
suite also passed repeated race checks. Two older key-cursor tests now reset
their process-global cursor explicitly so repeated test runs remain isolated.
The schema-error test now expects bounded quota recovery while continuing to
reject accidental schema fallback or raw error leakage.

Before and after: Go formatting/vet/full tests, full race suite against isolated
PostgreSQL, frontend clean install/typecheck/lint, all 53 frontend tests and
production build passed. Final Go server build passed. PostgreSQL integration
tests also passed uncached against `story_image_settings_00036_check`; no
production database writes or migrations were needed. The same 14 pre-existing
npm audit findings remain.

No real-provider quota flood or manual browser/local-model run was performed
for this point. Controlled faults prove the transport and priority behavior;
they do not quantify a real-provider latency improvement. Retry delays
deliberately increase time between rejected requests; aggregate throughput and
tail latency under live contention still require measurement.

## Later live correction

The stage 3.10 rerun exposed an overly broad long cooldown: one terminal 429
was followed by a next role waiting 179.7s with zero HTTP requests before manual
interruption. The first implementation shared even multi-hour Retry-After hints
across all keys/models on the hostname. This was a runtime latency defect, not a
model inference measurement. The interrupted report and verified fixture cleanup
are retained in `benchmarks/turn-after-interrupted-2026-11-10.json`.

Long hints now use a model/credential identity with a SHA-256 key digest;
credential values never enter diagnostics. Ordinary short backoff, aggregate
capacity, priority and the three-retry budget remain shared/bounded. Tests verify
key rotation after a one-hour hint, independence of another model, remembered
cooling credentials across clients, and prompt failure with no additional POSTs
when all credentials are cooling.

## Next point

Stage 3.8: generation profiles with quality as the default. Reconcile the old
profile proposal with the current planner and removed lore-guard path first.
An asynchronous speed profile requires a consistency design for Canon, saves,
forks, retries and the next turn before implementation.
