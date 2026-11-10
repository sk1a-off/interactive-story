# Experimental ChatGPT browser bridge — 2026-11-10

This is a separate **synthetic-only transport experiment**, not a new production story provider. No story settings, database, prompt bundles, credentials, or generation routing are changed.

## Run

From the repository directory:

```bash
./start-chatgpt-bridge.sh
```

Open <http://127.0.0.1:8791>. The dashboard serves the userscript and offers one-request and three-request tests, worker status and JSON report export. The token is generated in memory on every bridge startup; copy it from the dashboard into the userscript menu. It is a local transport token, not a ChatGPT credential.

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
- Worker status survives as a dashboard record for five minutes without heartbeat. Jobs are bounded to 60 in-memory records. Restart discards this test queue and rotates the token; keep exported reports if needed. This is not a durable production queue.
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

Queue tests cover exclusive concurrent claims, configurable concurrency, wrong leases, repeated send authorization, idempotent result upload, expiry without requeue, mismatched JSON, terminal-state preservation, worker attention and HTTP access/body limits. Eighteen DOM tests cover successful model selection, lost authorization, stopping during authorization, unrelated transcript, rejected result, interrupted page, missing temporary mode, unavailable model, personalized chats and missing personalization controls, clicking Start with an unchanged URL or only a changed fragment, ignored model selection, unmounted model lists, absent live announcements and an already open model menu. DOM tests are simulated contracts, not browser integration proof.

## Userscript 0.1.1: Start-button fix

The original Start handler assigned the fresh-chat URL even when that exact page was already open. Browsers do not reload for that navigation, so the armed flag changed but the worker did not start. Start now calls the worker directly in an already empty Temporary Chat. Otherwise it opens the fresh page, using an explicit reload when only the fragment differs. The button also has explicit styling independent of the page's button reset rules.

To update an already running setup: restart the local bridge so its embedded assets are rebuilt, reopen its userscript link and approve updating the existing script to **0.1.1**, reload the worker tabs, then configure the new local token (bridge restart rotates it). Starting in the current empty Temporary Chat should immediately change the button to **Остановить**. Browser integration still requires checking this in Chromium; the two new regression tests exercise the actual click handler against DOM fixtures.

## Userscript 0.1.2: remove personalization requirement

Per the user's request, remove all personalization menu clicks and pre-send checks. Personalized Temporary Chats and chats without this control may proceed, while a fresh empty Temporary Chat and GPT-6 Instant remain mandatory. Two regression tests verify successful sending with personalization enabled or its control absent, and assert zero clicks on personalization. Update the installed userscript to **0.1.2**; when restarting the bridge, copy its newly rotated local token again.

## Userscript 0.1.3: staged model confirmation

Do not require the model radio row and reasoning-power announcement to coexist in the same menu view. Confirm GPT-6 while the model list is visible (reopen to verify if selection changed), then confirm the active slider at minimum and the visible Instant label. Live status announcements are not required. The observed slider input has `aria-hidden=true` itself; its active container, rather than the input's own ARIA visibility, determines which slider to read. Normalize an already-open menu and close it on failure. Errors identify the failed selection stage. DOM fixtures now unmount the model list and include the real slider's ARIA visibility. Live verification in the user's Chromium is still pending; screenshots alone do not identify exactly which old condition failed.

## Userscript 0.1.4: dismiss the model menu

The user's screenshot identifies the failed stage as closing the menu while Instant is selected. Replace repeat trigger clicks with Escape, then an outside pointer interaction on the empty composer if needed. Verify menu dismissal and the visible Instant label before sending. Cleanup uses the same dismissal path. Three regression scenarios model a trigger that cannot close on click: Escape works, only outside interaction works, or both are ignored (the last must stop without sending). The panel displays version 0.1.4 to make installation status visible. These are DOM tests; actual Chromium userscript behavior still needs a live rerun.
