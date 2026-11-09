# Fast state activation — 2026-09-10

User explicitly approved switching state evaluation. Added
`GENERATION_FAST_STATE_V1` to config, Make and Compose; enabled in local `.env`.
Absent configuration remains off. In Safe Hybrid, only state_evaluator is newly
routed to Flash-Lite; Writer/world/quest remain on quality. Existing fast-error
fallback behavior is preserved; cancellation/deadline errors do not fall back.

Active AI revision 26 was verified to have storyLlmSafeHybrid=true. No AI or
prompt revision was changed; active state prompt remains revision 22. Journal
isolation, planner and provisional text switches remain enabled.

The previous server and PostgreSQL were stopped at the start of this turn.
Started existing deploy-postgres-1 and backend unit story-backend-fast-state
from backend/bin/server-fast-state on 127.0.0.1:8787. Verified readiness and
running environment flags. Full Go suite and routing/config regression tests pass.

## Synthetic full-pipeline check

Artifact: `tools/local-llm-benchmark/results/fast-state-activation-20260910-cSalWq/pipeline.json`.
Uses active prompts, fast state and enabled isolation; all Canon is in memory.
This validates the equivalent application configuration, not HTTP generation
on a user's story. Server factory wiring is covered separately by routing tests.

- Spend coins: full turn 44.128 s.
- Consume last ration: full turn 38.930 s.
- Both pass all required journal event/state assertions. State calls take
  1.074–1.303 s. No repairs, escalations or model transport failures.

Two successful runs do not establish a stable latency guarantee or a causal
speedup over the entire deployed configuration. Runtime is dominated by other
model roles. No user story was created for this check.

Next: test an isolated world prompt correcting missing evidenceQuote in the JSON
example, and clarify the forge fixture's allowed entities. Then address quest
completion evidence and recoverable attempt versus terminal failure. Neither
world nor quest fast routing was activated.

Rollback: set GENERATION_FAST_STATE_V1=false and restart the server. The current
systemd unit uses explicit environment variables, so editing .env alone will
not alter that unit; update its launch environment or use make backend-dev.
