# Speed profile: deferred-evaluation consistency review

## Status

Design review for stage 3.8. The asynchronous `speed` profile is **deferred**.
The API rejects creation/activation/execution of this profile; the UI marks it
unavailable. The existing early-text and streaming settings remain usable.

The original proposal asks to publish text and choices before world, journal and
quest evaluation, then finish those mutations before the next turn. This is not
safe as a simple goroutine launched after `Pipeline.Run` returns. No such
background commit path is added here.

## Current guarantees and evidence

`backend/internal/application/generation/pipeline.go` evaluates proposed changes,
builds choices with normalized journal/objective changes, validates events, then
appends one batch at the expected head. The batch includes text, choices and
state effects. The Google path already overlaps independent evaluators; choices
wait for journal/quest results while overlapping world evaluation.

`backend/internal/application/canon/service.go` locks the timeline, checks the
expected head and commits the batch transactionally. No public half-committed
beat currently exists.

`backend/internal/application/generation/durable.go` resolves the job's pinned
AI/prompt revisions and waits for `Pipeline.Run`. The job worker keeps its lease
and heartbeat through execution, then marks completion or schedules retry.
Returning early would detach evaluations from that recovery contract.

`backend/internal/application/saves/service.go` locks the timeline and snapshots
the committed head. A save during generation records the previous complete
head. Forks inherit replayable state at a chosen head, not provisional updates.

`frontend/src/lib/readerFlow.ts` unlocks a finished turn only when a new beat,
advanced head and matching four-choice set are visible. Streaming/draft updates
remain explicitly provisional and cannot unlock actions.

The dependency audit in `PARALLEL_DEPENDENCY_AUDIT_2026-11-09.md` already found
that removing choice dependencies gives little measured scheduling benefit and
changes choice inputs. The current profile work preserves that dependency.

## Failure cases for an early Canon commit

| Trigger | Consequence without a finalization protocol |
| --- | --- |
| Beat/choices committed before effects | A reader can select an action using outdated inventory, currency or quest status. |
| Save/fork before effects arrive | The save permanently captures text without the state changes implied by that text. A later commit on the parent cannot repair the fork. |
| Director edit or another mutation advances the head | Evaluations from the old context either fail expected-head validation or apply obsolete assumptions if that check is bypassed. |
| Crash after early text commit | The current job cannot safely rerun the entire pipeline without detecting which half was committed. |
| Worker completes while background evaluations still run | Lease recovery no longer owns those evaluations; failure and shutdown can silently lose them. |
| Cancellation/retry after visible choices | Stale choice IDs or draft revisions can survive unless the client and server both reject the previous attempt. |
| Evaluator outage or rejected proposal | The system must explicitly decide whether to retain, retract or finalize the visible turn. Skipping effects silently is not a general recovery policy. |

## Candidate protocol for future implementation

Keep one final atomic Canon commit. A visible pending turn would be a separate,
durable proposal associated with job ID, timeline ID, expected head, attempt
epoch, pinned AI/prompt revisions and a proposal hash. It must never masquerade
as an ordinary committed beat or advance the timeline head.

States would be `preparing → visible_pending → evaluating → ready_to_commit →
committed`, plus terminal failed/cancelled/stale states. Each transition needs an
atomic compare-and-set with the job lease/attempt and idempotent recovery. A
process restart must resume a persisted stage rather than replay an already
committed part or lose work. Final events would still enter Canon together.

Required behavior before enabling:

1. Early choices are labelled pending and remain inactive until finalization,
   or a durable action reservation explicitly waits and revalidates the choice
   after finalization. The current action/choice API has no such reservation.
2. Any early choice generation uses a separate provisional contract. Choices
   are revalidated against final journal/objective state; replacement IDs must
   invalidate old selections. Existing validated-choice inputs must not simply
   be dropped to reduce latency.
3. Saves and forks continue to use only complete Canon heads. If a user requests
   a save containing the pending text, the operation waits or clearly reports
   that finalization is incomplete; it cannot save a partial future head.
4. Director mutations and competing turns use a server-side finalization
   barrier or explicitly invalidate the proposal. Client-only button disabling
   is insufficient. Stale evaluations cannot be rebased without regeneration.
5. The generation job stays running, leased and cancellable until final commit.
   Reconnect/retry replays the current durable proposal and rejects stale epochs.
6. Timeout/cancellation clears the pending choices, preserves the previous Canon
   head, and exposes an explicit recoverable failure. It cannot report completion
   while effects remain outstanding.

Showing inactive provisional choices while retaining the current single job
could avoid a split Canon commit, but does not make the next turn available
earlier. The current writer preview already offers early text. There is no
measured benefit yet to justify another provisional-choice lifecycle.

## Acceptance gates still missing

- Production-shaped choice-quality comparison with/without evaluator results,
  including purchases, spending, item transfer/loss, ability limits and quest
  completion. An elapsed-time reduction alone is insufficient.
- Durable proposal schema, stage recovery and atomic barrier design, with
  existing migration files untouched.
- Save/fork/Director/next-turn tests during every pending state and concurrent
  finalization, including expected-head changes.
- Kill/restart/lease-loss tests before and after every durable transition, plus
  cancellation, duplicate finalization and model outage.
- Reconnect/mobile UI tests proving stale choices never become actionable and
  failed pending text is not presented as saved Canon.

Until those gates pass, `speed` must remain unavailable. Quality and balance
retain the current atomic commit and recovery semantics. No lore guard or world
rules path is reintroduced by this design.
