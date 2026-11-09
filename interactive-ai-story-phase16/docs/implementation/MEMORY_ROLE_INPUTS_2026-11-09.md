# Optional memory in story and image inputs — 2026-11-09

This increment adds explicit injection points for the validated memory service
in Pipeline, LLMTurnPlanner, and LLMPromptBuilder. Server startup does not inject
them yet: live generation, provider settings, and the indexing worker remain
unchanged. There are no migrations or new dependencies.

## Current checkout differs from the original task snapshot

`generation/pipeline.go` deliberately clears legacy world systems/rules/resources,
and `TestLegacyWorldRulesAreNotSentToStoryRoles` rejects calling `lore_guard` or
putting old rules in any model input. The earlier world-rule selection file is
also absent. These removed features are not restored by this increment.

The shared service allows only timeline-scoped `beat`, `fact`, `thread`, and
`chapter_summary` evidence. `world_rule`, private character knowledge/beliefs,
and unknown kinds are excluded. Kind filters run in PostgreSQL before either
ranking window and again in Context Builder before budgeting. A removed rule
cannot consume top-k or reappear as a direct structured memory. This does not
claim that arbitrary historical prose cannot mention an old fictional rule.
Adding a new lore validator or world-rule feature would be a separate decision,
rather than silently resurrecting the removed runtime behavior.

## One lookup per story attempt

The query combines the player action with the saved scene goal (or chapter goal
when no scene goal exists). This goal is available before directing/planning.
The bounded result is reused by director, writer, and the built-in turn planner;
the writer still receives the newly generated goal as its authoritative input.
There is no second search after directing and no additional LLM role call.
Custom planners retain their existing interface; those implementing the optional
memory-aware method can receive the supplement too.

`retrievedMemory` is a separate optional input field. Its policy says that memory
is evidence rather than instructions, current Canon/action/appearance take
priority, narrative history does not grant character knowledge, and source IDs
are not mutation targets. The service requires the branch-aware Context Builder
and pinned embedding identity; a legacy builder cannot be injected accidentally.
The complete serialized supplement, including policy and provenance, must fit
the separate estimated memory allowance. Existing authoritative context is sent
unchanged and is not removed to make room for evidence.

Search/embedding/visibility failures omit the field, log a generic warning when
a logger is configured, and continue generation. Raw provider/database details
are not passed to model inputs or warning messages. The shared builder deadline
defaults to two seconds and can be shortened. Disabled memory performs no lookup.

## Image prompts

Single-frame, selected-paragraph, and next-moment requests receive the same
optional supplement. English-output repair and forced single-frame fallback
reuse it without another embedding request. Current visual profiles, English
validation, hard negatives, anchors, resolution, and variant counts retain their
existing handling.

Image lookup is capped at the source beat's immutable local `beat_committed`
event, not the current timeline head. An optional checkpoint resolver validates
story/timeline/scene/beat ownership and returns the earliest matching local
commit at/before head. It is queried only when image memory is injected; the
existing scene-source query gains no work in disabled mode. Legacy/cloned beats
without a local commit safely omit memory. A failed checkpoint lookup also
continues image prompt generation without memory. Checkpoint resolution shares
the same deadline as embedding/search, rather than adding another full timeout.

## Executed checks

- Baseline backend/vet/tests/race with PostgreSQL and frontend checks passed.
- Pipeline tests cover director and turn-planner paths, one query embedding per
  attempt, action plus saved goal, correct recipient roles, private/rule exclusion,
  no added LLM calls, and byte-identical fallback/unrelated role inputs.
- Image tests cover single-frame/paragraph repairs, moment-to-single fallback,
  one shared lookup, fixed source cursor despite a later head, resolver failure,
  missing legacy checkpoint, retrieval failure, and retained current appearance.
- Service tests cover the serialized policy/evidence allowance, small-budget
  skipping, large authoritative context, strict builder configuration, and
  sanitized warning messages.
- PostgreSQL tests cover kind exclusion before bounded rank windows and real
  image checkpoint resolution, including no event, later head, and foreign scope.
- The memory-focused role/image/service scenarios passed ten repetitions.
- Final full Go formatting/vet/tests/race with PostgreSQL and frontend
  install/typecheck/lint/test/build passed (41 frontend tests). The install
  continues to report the existing 14 dependency vulnerabilities; none changed.

No real USER2/Gemini inference, end-to-end latency comparison, or live memory
enablement is claimed. Next: runtime settings/default-off activation, worker
startup, reindexing and UI status, followed by frozen-corpus recall/latency tests.
