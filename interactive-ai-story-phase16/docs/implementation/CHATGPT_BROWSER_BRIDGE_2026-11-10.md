# Experimental ChatGPT browser bridge — 2026-11-10

This is a separate **synthetic-only transport experiment**, not a new production story provider. No story settings, database, prompt bundles, credentials, or generation routing are changed.

## Run

From the repository directory:

```bash
./start-chatgpt-bridge.sh
```

Open <http://127.0.0.1:8791>. The dashboard serves the userscript and offers one-request and three-request tests, worker status and JSON report export. The launcher persists its local token in owner-only `data/secrets/chatgpt-bridge-token`; copy it from the dashboard into the userscript menu once. `CHATGPT_BRIDGE_TOKEN_FILE` overrides the file. Directly starting the binary without this variable retains ephemeral-token behavior. It is a local transport token, not a ChatGPT credential.

Visible Chromium windows can be opened separately:

```bash
./start-chatgpt-browsers.sh 3
```

The launcher uses `data/chatgpt-browser-profile`, separate from the normal browser profile. Install Violentmonkey from its official extension store in that profile, install the userscript from the local dashboard and sign in to ChatGPT once. All these windows share that profile, including the login and userscript settings. They are separate **windows**, not independent Chromium processes. Each worker tab has its own session UUID, shown on its panel. New chat navigation preserves that UUID. Avoid duplicating a busy tab: browsers can clone session storage; duplicate claims are rejected.

In each worker window choose **Запустить**. Position the visible windows on the second display using the desktop window manager. The launcher does not move, minimize or hide them. `CHATGPT_CHROMIUM_BIN` can select an installed Chromium executable. There are no headless, remote-debugging, sandbox-disabling, cookie-copying or CAPTCHA-solving flags.

Worker count and simultaneous job count are independent:

```bash
CHATGPT_BRIDGE_MAX_ACTIVE=3 ./start-chatgpt-bridge.sh
```

The default concurrency is 3; configuration accepts 1..4096, as does the window launcher. These are local bounds, not a claim about account capacity. The initial dashboard only queues batches of 1 or 3 synthetic probes. Idle workers poll; they do not invent requests.

## Per-request contract

1. Open a new root Temporary Chat. Confirm the temporary URL parameter, the UI mode and an empty transcript/composer. An ordinary chat is never a fallback.
2. Leave personalization controls alone. From userscript **0.1.2**, personalization is neither changed nor required for sending, as requested by the user after the control failed in Chromium. Temporary Chat may still use account personalization; it only provides a fresh conversation here.
3. Claim an exclusive queue lease. Select **GPT-6**, set power to **Instant**, verify the selected model while its list is visible, then minimum power and the visible Instant label. Recheck the visible Instant label before sending. An unavailable model/layout stops the worker.
4. Fill the exact synthetic prompt. Confirm no previous turns. Obtain a one-use send authorization, then click Send once.
5. Match the visible user turn to the prompt, allow exactly one assistant turn, wait for no Stop control, a Copy control and 2.5 seconds of stable text. Only then upload the reply under the job ID and lease.
6. The server validates the entire JSON object (optionally one JSON fence), unique probe ID, success value and exact synthetic sentence. A malformed/mismatched reply fails the job.
7. Navigate to a new Temporary Chat before taking another job. No conversation context is reused.

The userscript only arms explicitly marked worker tabs; it reads visible DOM controls/transcript, not cookies, account tokens, application internals or private endpoints. Selectors match the UI observed on 2026-11-10; unsupported layouts stop rather than silently using a different mode/model. The UI model name is verified; this is not a server-side attestation of internal model routing.

## Failures and attention

- Preparing job expiry becomes `failed`; after send authorization it becomes `uncertain`. Jobs are never automatically requeued, including before-send failures.
- Reload/crash with a pending lease pauses instead of resending. A lost authorization response never causes a click. Lost result upload does not cause another model request.
- A worker that cannot establish readiness pauses and reports `attention` when the local bridge is reachable. Expired heartbeat also shows no connection. CAPTCHA or login challenges are handled manually in the visible window; press **Запустить** afterwards. The bridge does not identify or solve a CAPTCHA automatically.
- Worker status survives as a dashboard record for five minutes without heartbeat. Jobs are bounded to 60 in-memory records. Restart discards this test queue; the launcher preserves the saved token; keep exported reports if needed. This is not a durable production queue.
- Localhost binding, exact Host validation, bounded request bodies, authenticated job operations and no CORS prevent ordinary remote pages from controlling the queue. Keep the dashboard/token local. Reports exclude lease secrets and the bridge token.
- Stop the bridge with Ctrl+C. Browser windows stay open for inspection. Stop a worker with its panel button; an already submitted ChatGPT request may still finish.

## Evidence and limits

Live UI probes in the Codex browser: **5/5** returned correct JSON and their own probe IDs. This includes one initial request, three requests dispatched into distinct Temporary Chats with overlapping execution, and one request with **Без персонализации** visibly confirmed before sending. All completed chats had one user and one assistant turn. The GPT-6 model and Instant power were separately inspected and selected in the visible model menu.

The initial four probes preceded the explicit personalization requirement and must not be presented as proof of unpersonalized execution. Timing was sampled after completion while development continued; it is an observation upper bound, not a latency benchmark or evidence of a speed improvement.

Raw, account-free observations: `benchmarks/chatgpt-browser-probes-2026-11-10.json`. Screenshot: `benchmarks/chatgpt-temporary-probe-2026-11-10.jpg`. The local dashboard was also opened in the browser and its three-request button created exactly three queued jobs; this UI check did not involve an installed userscript worker. Dashboard screenshot: `benchmarks/chatgpt-bridge-dashboard-2026-11-10.jpg`.

**Not yet validated live:** the full Violentmonkey → localhost → ChatGPT → result-upload path in the user's Chromium profile; automatic model selection through userscript-generated DOM events; actual CAPTCHA recovery; long prompts/production schemas; throughput and story quality. The available Codex browser has no userscript-manager installation surface. UI-only probes and DOM fixture tests do not establish these results.

Before production integration: complete the local dashboard's one-request and three-request runs in Chromium, inspect their exports, then test real pipeline roles and preserve their dependency graph. No production routing should be inferred from this prototype.

## Checks

```bash
env GOCACHE=/tmp/interactive-story-go-cache go -C backend test ./...
env GOCACHE=/tmp/interactive-story-go-cache go -C backend vet ./...
env GOCACHE=/tmp/interactive-story-go-cache go -C backend test -race ./cmd/chatgptbridge
# Existing frontend dev dependencies provide jsdom:
node tools/test_chatgpt_worker.mjs
bash -n start-chatgpt-bridge.sh start-chatgpt-browsers.sh
```

Queue tests cover exclusive concurrent claims, configurable concurrency, wrong leases, repeated send authorization, idempotent result upload, expiry without requeue, mismatched JSON, terminal-state preservation, worker attention and HTTP access/body limits. Twenty-nine DOM tests cover successful model selection, lost authorization, stopping during authorization, unrelated transcript, rejected result, interrupted page, missing temporary mode, unavailable model, personalized chats and missing personalization controls, clicking Start with an unchanged URL or only a changed fragment, ignored model selection, unmounted model lists, absent live announcements and an already open model menu. DOM tests are simulated contracts, not browser integration proof.

## Userscript 0.1.1: Start-button fix

The original Start handler assigned the fresh-chat URL even when that exact page was already open. Browsers do not reload for that navigation, so the armed flag changed but the worker did not start. Start now calls the worker directly in an already empty Temporary Chat. Otherwise it opens the fresh page, using an explicit reload when only the fragment differs. The button also has explicit styling independent of the page's button reset rules.

To update an already running setup: restart the local bridge so its embedded assets are rebuilt, reopen its userscript link and approve updating the existing script to **0.1.1**, reload the worker tabs, then configure the new local token (bridge restart rotates it). Starting in the current empty Temporary Chat should immediately change the button to **Остановить**. Browser integration still requires checking this in Chromium; the two new regression tests exercise the actual click handler against DOM fixtures.

## Userscript 0.1.2: remove personalization requirement

Per the user's request, remove all personalization menu clicks and pre-send checks. Personalized Temporary Chats and chats without this control may proceed, while a fresh empty Temporary Chat and GPT-6 Instant remain mandatory. Two regression tests verify successful sending with personalization enabled or its control absent, and assert zero clicks on personalization. Update the installed userscript to **0.1.2**; when restarting the bridge, copy its newly rotated local token again.

## Userscript 0.1.3: staged model confirmation

Do not require the model radio row and reasoning-power announcement to coexist in the same menu view. Confirm GPT-6 while the model list is visible (reopen to verify if selection changed), then confirm the active slider at minimum and the visible Instant label. Live status announcements are not required. The observed slider input has `aria-hidden=true` itself; its active container, rather than the input's own ARIA visibility, determines which slider to read. Normalize an already-open menu and close it on failure. Errors identify the failed selection stage. DOM fixtures now unmount the model list and include the real slider's ARIA visibility. Live verification in the user's Chromium is still pending; screenshots alone do not identify exactly which old condition failed.

## Userscript 0.1.4: dismiss the model menu

The user's screenshot identifies the failed stage as closing the menu while Instant is selected. Replace repeat trigger clicks with Escape, then an outside pointer interaction on the empty composer if needed. Verify menu dismissal and the visible Instant label before sending. Cleanup uses the same dismissal path. Three regression scenarios model a trigger that cannot close on click: Escape works, only outside interaction works, or both are ignored (the last must stop without sending). The panel displays version 0.1.4 to make installation status visible. These are DOM tests; actual Chromium userscript behavior still needs a live rerun.

## Userscript 0.1.5: hidden label placeholder (live DOM reproduction)

The failure is now reproduced on the actual ChatGPT DOM, rather than inferred from a screenshot. The closed picker (`aria-expanded=false`) returns `Уровень усилий рассуждения\nInstant` from `innerText`: the animated placeholder has `aria-hidden=true` and opacity zero, but still contributes to `innerText`. Consequently the former strict Instant comparison returns false even with the menu closed and Instant visible.

Read control text by skipping hidden/inert/ARIA-hidden branches and CSS-hidden or zero-opacity elements. Keep the strict visible mode match; a hidden stale Instant label must not authorize a visibly Medium control. The same corrected predicate was evaluated read-only against the live DOM: old check false, corrected check true, rendered label Instant. This validates the specific label correction, not the full userscript transport path.

Three additional tests cover the hidden placeholder, a hidden stale Instant label with visible Medium (must stop), and diagnostic redaction. The new **Диагностика** button copies only version, worker ID, control attributes, slider values and the last failed selection stage, with no prompt, transcript, lease or token. If clipboard access is unavailable, it displays that bounded report in the panel. Screenshot-based diagnosis should be replaced with this report for any further unsupported layout.

Read-only live observation: `benchmarks/chatgpt-control-label-regression-2026-11-10.json`. Update the userscript to **0.1.5** after restarting the bridge and reload worker tabs. Full Chromium integration remains to be rerun.

## Live role-test support and userscript 0.1.6–0.1.7

The attached user export confirmed the complete userscript path for **4/4 short probes**, across three workers with three overlapping authorized requests. See `benchmarks/chatgpt-user-probes-2026-11-10.json`. Total durations were 6.31–7.64 s; these small copy probes are not a production latency benchmark.

A separate authenticated `POST /v1/chat/completions` endpoint on **8791 only** now accepts buffered `gpt-6-instant` test calls with system/user text messages. It does not modify the main story server. It queues an exclusive browser lease, waits for exactly one JSON object and returns the content in a compatible response envelope. Streaming, tools, other models, native token accounting and enforced temperature/token-limit/schema settings are not supported. Browser completion maps to a transport `stop`, not a native model finish reason. Queue admission is bounded, prompt limit 48 KiB, HTTP body limit 64 KiB, and caller cancellation cannot requeue a sent request.

`tools/chatgpt_role_bench.py` uses the active prompt revision snapshot in `data/run/chatgpt-active-prompts.json` and the compiled `data/run/chatgpt-role-bench`. It first requires three fresh ready heartbeats, then runs three independent groups (16 synthetic cases). It never writes production settings or Canon. The underlying benchmark saves partial results after each case and stops its group on the first transport error. Token counts/TPS remain unavailable; test scores are mechanical checks, not a literary verdict.

Long-message failures were reproduced in live DOM:

- **Show more:** bubble text includes the expand button. For 4,941 input characters the old bubble comparison saw 4,956; `[data-search-result-target]` retained exactly 4,941. Version 0.1.6 reads only the message-content node. Strict mismatched-message rejection remains enabled.
- **Markup:** the real 4,399-character director input became 4,389 DOM characters, exactly removing ten backticks and producing five inline-code elements. Keeping the original contract logically unchanged requires render-safe JSON string encoding on the server. ASCII punctuation inside envelope strings is represented by JSON Unicode escapes; decoding gives the original message values. Outer JSON structure remains valid. The strict UI comparison is preserved; it is not replaced with substring matching.
- **Lost fragment:** version 0.1.7 retains a paused worker panel in tabs with a previously registered UUID even when ChatGPT drops `#story-bridge-worker`. Ordinary unmarked tabs remain untouched and registration alone does not start sending.
- **Trigger events:** version 0.1.7 sends the pointerdown/mousedown/pointerup/mouseup/click sequence when opening the model picker and waits for open state before selecting a model. Closure uses Escape/outside interaction. Bounded local diagnostics include only stage, error label and expected/actual lengths; no prompt, reply, lease or account token.

Uncertain or failed attempts are kept separately from any later successful rerun. They are never silently retried under their old lease. The live server launched during this test remains running under the PID stored in `data/run/chatgptbridge-live.pid`, using the same local token. Its log is `data/run/chatgptbridge-live.log`. Do not launch a second bridge on the occupied port. Stop that exact process before using the normal launcher again.

## Userscript 0.1.8 and completed role coverage

Version 0.1.8 expands a matching collapsed user-message prefix before correlating an answer. It never accepts only the prefix. A failed or incomplete expansion cannot upload a result. Multiple top-level message-content segments are combined when needed. A live DOM check confirmed 10,240 characters collapsed and an exact 11,677-character match after expansion. The benchmark has successful coverage of all 16 scenarios across retries; see [the result report](CHATGPT_ROLE_TEST_RESULTS_2026-11-10.md) for failures, latency and limits.

The bridge now defaults to a 2,000 ms lease warmup per fresh worker page. `CHATGPT_BRIDGE_LEASE_WARMUP_MS=0` disables it for explicit experiments; it is included in the reported timings. Menu opening is still not perfectly reliable and no production routing switch was made. The local fail callback carries bounded diagnostic labels and lengths, not transcript contents or account credentials.
