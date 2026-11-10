# Start the current host checkout

Use host mode for this existing installation: it keeps the saved Google keys in
`data/secrets/provider-keys.json` and the existing PostgreSQL stories. Prerequisites
are Go 1.25+, Node.js 22+, Docker/Compose and a browser. The current active AI
revision uses Google Safe Hybrid, so llama.cpp is not required for this path.

The commands below start the application, not the benchmark. No test runner or
temporary benchmark embedding service is required for ordinary play.

## One-command launch

```bash
./start.sh
```

The executable launcher works from any current directory. It starts the existing
PostgreSQL container, waits for readiness, verifies a new private database dump
before pending migrations, installs frontend dependencies when their lockfile
changes, builds the current backend, starts backend/Vite and opens the browser
after readiness checks. Subsequent launches skip backups/migrations when the
schema is already current. Saved credentials and generated files use the same
locations as `make backend-dev`.

Logs are kept in `data/logs/start-*`; backups in `data/backups/`. Ctrl+C or closing
the terminal stops launcher-owned processes while leaving PostgreSQL and
pre-existing services running. A lock prevents duplicate instances. Occupied
application ports cause a clear failure without killing their owners.

If saved AI settings enable memory, USER2 is started automatically; missing CPU
dependencies/model files are downloaded on first use. An already healthy USER2
service is reused. Perchance remains a browser worker: keep its configured tab
and userscript open. Narration requires your separately configured OmniVoice
service. The launcher does not download a local story GGUF or change AI settings.

Use `./start.sh --no-browser` for terminals without a browser. Advanced overrides
are `HOST_DATABASE_URL` (local PostgreSQL only), `STORY_DB_CONTAINER`,
`STORY_BACKEND_PORT`, `STORY_FRONTEND_PORT` and the existing `HOST_*` paths.
Selected `.env` values are read as literal data; the file is never sourced.

Validation: a fresh isolated database was migrated to 35, then the launcher
created/validated its dump and upgraded it to 40. Backend readiness and the
frontend API proxy worked; duplicate launch returned without new services, and
Ctrl+C released both application ports while PostgreSQL remained healthy.
Automated stub-based tests cover failed-backup migration blocking, literal
dotenv handling and preserving an occupied port. No production migration or
model generation was performed by these launcher tests.

The manual steps below remain available for diagnosis.

## 1. Prepare the database and frontend once

From the project directory:

```bash
cd /home/sk1a/Projects/interactive-ai-story-phase16-result/interactive-ai-story-phase16
docker start deploy-postgres-1
for attempt in $(seq 1 30); do
  docker exec deploy-postgres-1 pg_isready -U story -d story && break
  sleep 1
done
mkdir -p data/backups
docker exec deploy-postgres-1 pg_dump -U story -d story -Fc > "data/backups/story-$(date +%Y%m%d-%H%M%S).dump" && make migrate-up
npm --prefix frontend ci
```

The existing container name was verified. This path uses `docker start/exec`
because the current environment's Compose `up --wait` command exited with signal
11; the direct Docker commands work. For a fresh installation that does not have
this container, use the general [local guide](RUN_LOCAL.md) first.

Stop if the backup command fails; migration only follows a successful dump.
Keep the dump. The working database was at
migration 35 when checked; the application now needs migrations through 40.
Re-running `make migrate-up` after reaching 40 is a no-op. Existing migration files
are unchanged. Run migrations before starting the updated backend.

The commands use this installation's local `story` database/user defaults.
`HOST_DATABASE_URL` overrides the database DSN for migration/backend commands
if intentionally using another database.

## 2. Start backend — terminal A

```bash
cd /home/sk1a/Projects/interactive-ai-story-phase16-result/interactive-ai-story-phase16
make backend-dev
```

Leave this terminal running. It listens on `http://127.0.0.1:8787` and uses the
saved active AI revision and host credential file. The launcher reads the four
existing generation switches from `.env`. The story attempt deadline defaults
to 25 minutes and can be overridden with `GENERATION_EXECUTION_TIMEOUT`; this is
a maximum budget, not a delay imposed on requests.

## 3. Start frontend — terminal B

```bash
cd /home/sk1a/Projects/interactive-ai-story-phase16-result/interactive-ai-story-phase16
HOST_FRONTEND_HOST=127.0.0.1 make frontend-dev
```

Leave this terminal running, then open `http://127.0.0.1:5173`.
AI Settings should show the saved Google/Safe Hybrid revision and a configured
key count. Keep Quality as the default profile; Balance has no additional call
reduction when the existing combined planner is already enabled. Native writer
streaming is a separate opt-in setting. Speed is not available yet.

## 4. Connect illustrations

Keep your configured Perchance generator named `interactive-ai-story-worker`
open with the runtime from `tools/perchance/perchance-runtime-worker.html` and
the Violentmonkey script `tools/perchance/violentmonkey.user.js` enabled. The
script defaults to backend address `http://127.0.0.1:8787`. If a worker token is
configured, the script's token setting must match the backend environment.

In AI Settings, confirm the image worker is connected. Complete any verification
shown by Perchance manually. Story text can work while the image tab is offline.
See [the image-worker guide](PERCHANCE_IMAGE_WORKER.md) for first-time setup.

## Optional: enable vector memory

Memory is disabled in the currently saved revision. If enabling it, first run
the local CPU embedding service in a third terminal. Keep its environment/cache
under ignored `data/` rather than the temporary benchmark directories:

```bash
cd /home/sk1a/Projects/interactive-ai-story-phase16-result/interactive-ai-story-phase16
python3 -m venv data/user2-venv
data/user2-venv/bin/pip install --index-url https://download.pytorch.org/whl/cpu 'torch==2.15.0+cpu'
data/user2-venv/bin/pip install -r tools/embedding_provider/requirements.txt
HF_HOME="$PWD/data/user2-cache" OMP_NUM_THREADS=2 MKL_NUM_THREADS=2 TOKENIZERS_PARALLELISM=false data/user2-venv/bin/uvicorn tools.embedding_provider.app:app --host 127.0.0.1 --port 8090
```

First startup downloads the public USER2 model. Check
`http://127.0.0.1:8090/health` reports `deepvk/USER2-small`, 384 dimensions and CPU.
Then enable memory in AI Settings, use top-k 8 / token budget 1024 initially, and
save. Wait for the index preparation status to finish before expecting retrieval.
The localhost endpoint is used in host mode. The benchmark's temporary service
does not persist across an environment reset.

## Checks and shutdown

```bash
curl --fail --silent http://127.0.0.1:8787/health/ready
curl --fail --silent http://127.0.0.1:8787/api/v1/images/worker-status
```

Try New Story → Setup → Start → one continuation; verify Hero and choices update
after the full turn. Images require the connected Perchance tab. Narration
additionally requires your existing OmniVoice service (default URL
`http://127.0.0.1:6655`); this guide does not install it.

Stop frontend/backend/optional embedding service with Ctrl+C in their terminals.
To pause the database while preserving stories, use
`docker stop deploy-postgres-1`.

Do not switch this existing host installation to the full Docker launcher merely
to start it: that path uses a separate provider-secret volume and needs deliberate
credential provisioning. The host path above reuses the currently saved keys.
