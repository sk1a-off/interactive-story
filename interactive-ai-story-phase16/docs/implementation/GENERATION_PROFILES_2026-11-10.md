# Generation profiles — stage 3.8

## Implemented behavior

`PublicSettings.generationProfile` is persisted in the immutable AI revision,
independently of `StoryLLM.Profile` (provider metadata and model routing).
The default is `quality`; older revisions with an absent field also resolve to
quality without a database migration or rewriting existing settings.

| Profile | Planning | Evaluation/commit |
| --- | --- | --- |
| Quality | Existing behavior, including the current `GENERATION_TURN_PLANNER_V1` switch | Existing validators, dependency-aware evaluation, choices and one atomic Canon batch |
| Balance | Existing `turn_planner` combines action interpretation, direction and pacing regardless of that launcher switch | Same evaluators, dependencies, validators and atomic commit |
| Speed | Unavailable; creation, activation and runtime resolution reject it | Deferred pending [consistency gates](SPEED_PROFILE_DESIGN_2026-11-10.md) |

The UI restores/saves the profile, identifies it in revision history, and marks
speed unavailable. Saving creates a new revision; queued generations keep their
pinned revision. Activating an older revision restores its profile. Changing the
launcher planner switch remains an existing global behavior for quality; balance
always uses the combined planner.

No model-routing override is introduced. The existing safe-hybrid router decides
where `turn_planner`, choices and optional fast-state evaluation run. Writer,
world and quests keep their configured routes and escalation behavior. Memory,
streaming, schemas and token budgets remain independently configured.

## Reconciliation with the older proposal

- Reuse `turn_planner` rather than introduce a duplicate `planner` prompt role.
  It already returns intent, immediate goal and pacing with agency and structural
  validation. Its measured/deployed token cap remains 1600.
- One semantic writer rewrite already exists for repeated prose. Balance keeps
  that bound and the existing syntax-repair policy; persistent repetition fails
  without committing Canon.
- Lore guard and world rules were removed in previous accepted work. The old
  suggestion to make lore guard repair small violations is not applicable and
  does not restore those removed components.
- Existing role token budgets are retained. The profile selector is not evidence
  that new shorter budgets preserve quality; no additional truncation is enabled.

## Observed effect and verification

Production-Pipeline tests with valid deterministic responses verify eight role
calls for the legacy three-step planning path and six for combined planning.
Quality with the existing planner flag enabled also uses six calls. The local
launcher currently has `GENERATION_TURN_PLANNER_V1=true`, so switching this
installation from quality to balance alone provides no additional call reduction.
These are role counts, not live-model latency or quality measurements.

Tests cover old-config defaults and JSON round trip, profile validation,
create/activate rejection of unavailable profiles, immutable old revisions,
actual queued-job resolution after an active-profile switch, six planning/profile
combinations, one-rewrite failure without Canon changes, and unchanged
safe-hybrid routes with fast-state both off and on. Those Go packages passed
three race-detector repetitions. UI tests cover unavailable speed, old-config
fallback, restoring both supported profiles and saving without losing model,
streaming or schema settings.

Before and after: Go formatting, vet, all backend tests, the full race suite with
isolated PostgreSQL, server build, frontend clean install/typecheck/lint, all
57 frontend tests and production build passed. Production settings and schema
were not modified. No manual browser or real-provider profile comparison was
performed; this change reuses the existing deployed planner and preserves quality
defaults. The 14 pre-existing npm audit findings are unchanged.

## Next points

Stage 3.9: verify local llama.cpp protocol compatibility; add optional prompt
caching only if evidence justifies it. Stage 3.10: repeat the comparable turn
benchmark and report latency, tokens, retries and failures before/after without
treating the unavailable asynchronous profile as implemented.
