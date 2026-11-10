# Generation worker recovery and terminal replay

Additional queue/timeout work was reviewed and included with the user's approval.

## Behavior

- `GENERATION_EXECUTION_TIMEOUT` configures the story worker's per-attempt
  deadline, defaulting to 25 minutes instead of the old hardcoded 10 minutes.
  This provides headroom above the existing 20-minute fast-model HTTP timeout.
  It raises a cancellation budget; it does not delay ordinary requests or make
  model inference faster. The local Makefile and Compose pass the setting through.
- Heartbeat failures cancel the affected execution and report lease loss rather
  than impersonating parent shutdown. A lost lease does not stop the worker from
  taking its next job. An unexpectedly returned worker is restarted while the
  server's parent context remains live.
- Terminal attempt updates (`failed`, `completed`, `retrying`) use a detached,
  bounded five-second persistence context so an already-cancelled generation
  context does not discard its durable replay frame. Append errors are logged.
- Queue finalization adds durable failed frames for exhausted attempts and
  cancelled jobs if the pipeline did not already persist a terminal frame.
  Recovery of an expired exhausted lease does the same; retryable jobs remain
  queued without a false terminal frame. User cancellation through `Complete`
  is included, rather than leaving a preceding `retrying` frame as the last
  durable update.

No existing migration is edited and no new schema is required. Canon remains
atomic; generation status/replay improvements do not commit partial prose or
skip evaluators.

## Verification and limits

Tests cover timeout defaults/overrides/invalid values, continuing after a lost
heartbeat or DB heartbeat error, cancelled-context terminal persistence, and
PostgreSQL final failure, existing terminal idempotency, retryable failure,
expired-lease recovery and explicit user cancellation. Full formatting/vet/Go
tests, an uncached full race suite with isolated PostgreSQL, server build, and
frontend typecheck/lint/57 tests/production build passed.

The full-turn timing benchmark bypasses durable workers and SQL Canon writes;
its latency results do not measure these recovery changes. Manual browser
reconnect, real process kill/restart and multi-worker load acceptance remain
separate runtime gates.
