# Journal evidence isolation prototype

Implemented an opt-in `Pipeline.JournalEvidenceIsolationV1` experiment and a
synthetic-only benchmark flag `-journal-evidence-isolation`. No server config,
active prompt revision, model routing or user Canon was changed. The extractor
path retains its original all-or-nothing behavior.

The normal validator is tried first. On evidence failure, the fallback may
retain exactly one update of an existing active currency entry, discarding only
new item proposals with invalid evidence. It refuses removals, unknown IDs,
renames, duplicate update IDs, duplicate new item names, creates with IDs,
multiple surviving mutations, empty rejected evidence and overlapping quotes.
The surviving balance still passes the unchanged evidence and domain gates.
No approximate matching or additional LLM call is introduced.

This is deliberately not a general dependency resolver. Different IDs and
non-overlapping quotes cannot prove semantic independence. Exchange, crafting,
indirect transaction dependencies and misleading but verbatim balance evidence
need further acceptance coverage before enabling the feature. Production remains
unchanged. An invalid acquisition remains absent; salvaging money does not mean
the whole inventory is complete.

## Verification

Frozen inputs from `state-acceptance-20260909-sczJw2/pipeline.json` are copied to
`backend/internal/application/generation/testdata/journal-isolation-frozen.json`.
Tests replay its exact Writer text and state output through the full application
pipeline with scripted responses for other roles and an in-memory Canon sink.
This is an application integration replay, not a fresh live model or PostgreSQL
integration run.

- Default: coin update absent; final ration correctly removed (1/2 target states).
- Opt-in: currency updated to active/7; ration inactive/0 (2/2 target states).
- Invalid key creation remains absent; choices are committed after journal events.
- Eleven boundary cases passed, covering order independence and fail-closed
  conflicts, removals, negative balance, bad quote and unknown references.
- Full `go test ./...` passed with local test-server permission.

Next: evaluate fresh synthetic full-pipeline outputs and semantic dependency
counterexamples, and add explicit reporting of discarded mutations before any
production activation. Keep the experiment off by default.

## User-authorized activation — 2026-09-09

After explicit approval, wired `GENERATION_JOURNAL_EVIDENCE_ISOLATION_V1` through
config and server pipeline construction. Enabled it in local `.env`; Make and
Compose propagate it, while absent configuration and `.env.example` remain off.
Active prompt revision 22 changes only `state_evaluator` to the tested snapshot
`cmd/hybridbench/fixtures/state-shortquote-v1.md` (SHA-256
`01a2ecf1de9b09e3c3ca54ae194e39c65986fa752e9c1ac8210f302285fc64cd`).
All other prompts and role settings were verified preserved. Model routing was
not changed. Previous revision 21 (`a947404c-85b5-4aaf-8e45-2b095e34e28d`) remains
available; revision 22 ID is `14234c11-9baa-4039-a1ab-f6b97be71143`.

Backend started as user unit `story-backend-journal-isolation-host.service`,
binary `backend/bin/server-journal-isolation`, port 8787. Verified the running
unit has isolation, planner and provisional text flags enabled; `/health/live`
and `/health/ready` passed. The initial startup using `.env` as a systemd
EnvironmentFile failed because its Docker database hostname is unavailable on
the host; the successful unit uses explicit host connection settings.

Full Go tests passed. No new live story was generated after activation, and this
does not upgrade the preceding speed evidence into a causal speedup claim.
Disable the switch and restart the backend to revert isolation; activate prompt
revision 21 separately to revert the prompt. The current systemd unit has the
switch explicitly set, so changing `.env` alone does not change that running unit.
