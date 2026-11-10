# Interactive AI Story

Implementation repository for the v1.12 architecture specification.

The current local-MVP path includes the work through the Perchance browser image worker, semantic multi-moment illustration placement, runtime Prompt Studio, and replayable global/minor objectives. Migrations currently run through `00040`; branch-aware hybrid memory is connected to runtime settings and background indexing, disabled by default. The foundation includes append-only Timeline Canon, expected-head concurrency, snapshots, Facts/Knowledge/Beliefs separation, durable generation jobs, safe SavePoint forks, Director mode, real OpenAI-compatible Story LLM wiring, and immutable provider/prompt provenance. Objective lifecycle and generation rules are documented in [`docs/implementation/OBJECTIVES.md`](docs/implementation/OBJECTIVES.md).

See `docs/implementation/IMPLEMENTATION_STATUS.md` for the phase history and `docs/implementation/PROJECT_READINESS.md` for the current verified gates.

Development commands, environment variables, migrations, and CI checks are
documented in [`docs/implementation/DEVELOPMENT.md`](docs/implementation/DEVELOPMENT.md).

For this existing host installation, use the
[current startup steps](docs/implementation/START_CURRENT_2026-11-10.md).
Run `./start.sh` for one-command database/backend/frontend startup and automatic
browser opening; Ctrl+C stops the application processes.
The [completed before/after report](docs/implementation/TURN_AFTER_RESULTS_2026-11-10.md)
records successful quota recovery but no demonstrated overall speedup.


## Current local MVP status

The project now includes the implemented path:

`Story Setup → Main Timeline → Reader + Hero journal → durable Story generation jobs → Saves/Forks → Director → Perchance Scene image generations`

The Reader keeps a branch-local Canon journal of main/side quests, protagonist abilities, characteristics, spendable currencies and carried inventory. Confirmed purchases, rewards and losses change the balance and immediately constrain further generation.

Current image integration is the browser-worker architecture:

`Go/PostgreSQL ↔ Violentmonkey ↔ DOM mailbox ↔ Perchance generateImage()`

Image generations use Digital Painting with configurable resolution (768×768,
512×768, or 768×512) and 1–4 variants. Defaults remain 768×768 and two variants.
AI settings changes affect only new image generations. Story text never waits
for image completion. The visual prompt pipeline uses a layered
Perchance/Flux-friendly natural-language scene prompt, a separately pinned style
layer, a separately merged negative layer, and strips provider control tokens
from model output.

For first launch, use:

- [`docs/implementation/RUN_LOCAL.md`](docs/implementation/RUN_LOCAL.md)
- [`docs/implementation/PERCHANCE_IMAGE_WORKER.md`](docs/implementation/PERCHANCE_IMAGE_WORKER.md)
- [`docs/implementation/URVILLAIN_PROMPT_RESEARCH.md`](docs/implementation/URVILLAIN_PROMPT_RESEARCH.md)

## Google AI Story LLM

Open **AI Settings** from the start screen, select **Google AI**, then choose Gemini 3.6 Flash, Antigravity, Gemini 3.1 Flash-Lite, Gemini 3.5 Flash-Lite, or Gemma 4 31B. Paste one or more keys created in [Google AI Studio](https://aistudio.google.com/app/apikey) (one key per line) and choose **Save and enable selected model**. Regular Gemini and Gemma models use Google's OpenAI-compatible endpoint; Antigravity uses the Interactions API because it is a managed agent rather than a chat-completions model. A key is mandatory before a Google AI revision can be activated.

Enable **Safe hybrid** to route only action interpretation, pacing, and choices through Gemini 3.5 Flash-Lite. Director, writer, world, journal, quests, setup, repairs, and image prompts remain on Antigravity. If a fast-role request fails, it is retried through Antigravity automatically. The setting is stored in the immutable AI revision; previously queued jobs keep their original revision.

On a `429` quota/rate-limit response the backend applies exponential jittered backoff, rotates Google credentials, and remembers the cursor. At most three quota retries are allowed per role call, including protocol fallbacks. It also handles the successful-HTTP error envelope sometimes returned by Google's compatibility endpoint. Rejected credentials are skipped, while ordinary model, JSON, and server errors do not rotate credentials.

Runtime clients share a request gate per provider/origin: at most three Google calls or one local OpenAI-compatible call, including open stream bodies. Waiting player requests take priority over illustration prompts, and quota cooldown waits consume no request slot. See [`docs/implementation/REQUEST_LIMITS_2026-11-10.md`](docs/implementation/REQUEST_LIMITS_2026-11-10.md) for retry bounds, verification, and scope.

Gemini quotas are generally enforced per Google Cloud project rather than per key. Multiple keys only provide additional quota when they belong to projects or accounts with independent limits; keys from one project normally share the same RPM, TPM, and RPD budget.

Provider keys are write-only in the HTTP API and never enter Story JSON or PostgreSQL. For the local launcher they are persisted in `data/secrets/provider-keys.json` with owner-only permissions (`0600`); Docker uses the dedicated `provider-secrets` volume. Override the location with `SECRET_STORE_PATH` or `HOST_SECRET_STORE_PATH`.

AI Settings also offer **Quality** (current behavior, default) and **Balance**
(combined turn planning). If combined planning is already enabled, Balance does
not remove more calls. **Speed** remains unavailable pending durable finalization
and save/fork safety work. Models and early-text settings stay independent of
the profile. See [generation profiles](docs/implementation/GENERATION_PROFILES_2026-11-10.md).

The project is suitable for a local MVP launch attempt. It is not yet marked production/release PASS until the PostgreSQL + real LLM + browser + Perchance acceptance gates are executed on the target machine.
