# Local launch guide

This is the recommended launch mode for the current MVP because the Story LLM and Perchance worker both live on the host machine.

## Prerequisites

Install:

1. Go 1.25+
2. Node.js 22+
3. Docker Desktop / Docker Engine with Compose
4. `llama.cpp` with `llama-server`
5. the selected GGUF Story model: `Vikhr-Nemo-12B-Instruct-R-21-09-24 Q5_K_M`
6. a Chromium/Firefox browser with Violentmonkey
7. a Perchance generator where `generateImage = {import:text-to-image-plugin}` works

First-time dependency installation needs Internet access.

## 1. Start PostgreSQL + pgvector

From the project root:

```bash
docker compose -f deploy/docker-compose.yml up -d postgres
```

Database is exposed only on `127.0.0.1:5432`.

## 2. Download Go dependencies

```bash
cd backend
go mod tidy
cd ..
```

This also creates `backend/go.sum`, which is not pre-generated in the sandbox artifact.

## 3. Run all migrations

Bash:

```bash
export HOST_DATABASE_URL='postgres://story:story@127.0.0.1:5432/story?sslmode=disable'
make migrate-up
```

PowerShell:

```powershell
$env:HOST_DATABASE_URL='postgres://story:story@127.0.0.1:5432/story?sslmode=disable'
cd backend
go run github.com/pressly/goose/v3/cmd/goose@v3.24.2 -dir db/migrations postgres $env:HOST_DATABASE_URL up
cd ..
```

Migrations currently run through `00040_memory_runtime_profiles.sql`.
See `DEVELOPMENT.md` for the complete ordered list.

## 4. Start the Story LLM

Example:

```bash
llama-server \
  -m "/path/to/Vikhr-Nemo-12B-Instruct-R-21-09-24-Q5_K_M.gguf" \
  -c 16384 \
  --cache-type-k q8_0 \
  --cache-type-v q8_0 \
  --host 127.0.0.1 \
  --port 8081
```

Windows example:

```powershell
.\llama-server.exe -m "D:\models\Vikhr-Nemo-12B-Instruct-R-21-09-24-Q5_K_M.gguf" -c 16384 --cache-type-k q8_0 --cache-type-v q8_0 --host 127.0.0.1 --port 8081
```

Use the hardware/offload flags established by the project benchmark if required. The application only requires the OpenAI-compatible endpoint on port 8081.

Verify:

```bash
curl http://127.0.0.1:8081/v1/models
```

## 5. Start backend on the host

From project root:

```bash
make backend-dev
```

Equivalent Bash:

```bash
cd backend
DATABASE_URL='postgres://story:story@127.0.0.1:5432/story?sslmode=disable' \
STORY_LLM_BASE_URL='http://127.0.0.1:8081' \
HTTP_ADDR='127.0.0.1:8787' \
MEDIA_ROOT='../data/generated' \
MEDIA_BASE_URL='/media' \
go run ./cmd/server
```

To open the application from a phone connected through WireGuard, see
"Remote access" below; do not bind the backend to all interfaces without the
access tokens.

Backend endpoints:

- health: `http://127.0.0.1:8787/health`
- API: `http://127.0.0.1:8787/api/v1/...`
- generated files: `http://127.0.0.1:8787/media/...`

Verify:

```bash
curl http://127.0.0.1:8787/health
curl http://127.0.0.1:8787/internal/image-worker/status
```

Before the browser worker is opened, worker status should report `connected: false`.

## 6. Install frontend dependencies and start Vite

First time:

```bash
cd frontend
npm ci
cd ..
```

Run:

```bash
make frontend-dev
```

or:

```bash
cd frontend
npm run dev
```

The Makefile and `start.sh` bind Vite to the local machine by default:

```bash
HOST_FRONTEND_HOST='127.0.0.1' make frontend-dev
```

### Remote access (phone over WireGuard, LAN)

The API has no user accounts. Anyone who can reach the backend, or the Vite
dev server that proxies `/api`, `/internal` and `/media` to it, can read and
change every story and the AI settings. Remote access is therefore gated by two
tokens:

1. Put long random values in `.env`:

   ```bash
   API_ACCESS_TOKEN='<long random value>'
   IMAGE_WORKER_TOKEN='<another long random value>'
   ```

   Both are required together: the worker routes are exempt from the API
   token so the userscript keeps working, hence they need their own.
   `start.sh` and `make backend-dev` forward both. With `API_ACCESS_TOKEN`
   set, every route except `/health*` requires it, regardless of the bind
   address. The Perchance userscript keeps using `IMAGE_WORKER_TOKEN` for
   `/internal/image-worker/*`.
2. Keep the backend on loopback and expose only the Vite dev server:

   ```bash
   HOST_FRONTEND_HOST='0.0.0.0' make frontend-dev
   ```

   (`./start.sh` honours the same variable from `.env`.) Binding the backend
   itself to a non-loopback address is also possible, but it refuses to start
   unless both tokens are set or `HTTP_TRUSTED_NETWORK=true` acknowledges that
   the network restricts access.
3. On the phone, open once:

   `http://10.10.10.2:5173/api/v1/auth/session?token=<API_ACCESS_TOKEN>`

   It stores an HttpOnly, SameSite=Strict session cookie and redirects to the
   application. Tools can send `Authorization: Bearer <API_ACCESS_TOKEN>`
   instead. `DELETE /api/v1/auth/session` clears the cookie.

Generated images under `/media` are covered by the same token, and directory
listings are disabled.

Open:

`http://127.0.0.1:5173`

Vite proxies `/api`, `/internal`, `/health` and `/media` to backend port 8787.

## 7. Prepare Perchance runtime

Create or rename the dedicated generator to exactly:

`interactive-ai-story-worker`

The strict name is intentional: the userscript does not run on unrelated Perchance pages and never broadcasts Story prompts into arbitrary frames.

In the generator, keep the working imports, including:

```text
generateImage = {import:text-to-image-plugin}
```

If you also use the framework UI:

```text
generateInterfaceHTML = {import:t2i-framework-plugin-v2}
```

Paste the contents of:

`tools/perchance/perchance-runtime-worker.html`

into the generator HTML/runtime where `generateImage()` is actually available.

The runtime performs exactly:

- Digital Painting style
- the resolution pinned by the backend: 768×768, 512×768, or 768×512
- 1–4 `generateImage()` calls in `Promise.all`, matching the pinned variant count

It receives a scene prompt, a frozen style layer, and a separate negative layer. Do not embed `(resolution:::)`, `(seed:::)`, `(guidanceScale:::)`, or `(negativePrompt:::)` tokens in story prompts; the backend strips them and the worker owns validated provider options.

Do not add localhost fetch logic to Perchance.

## 8. Install Violentmonkey transport

Create a new Violentmonkey script and paste:

`tools/perchance/violentmonkey.user.js`

Save/enable it and keep the Perchance generator tab open.

It polls:

`http://127.0.0.1:8787/internal/image-worker/jobs/next`

using `GM_xmlhttpRequest`, writes the job to the DOM mailbox, reads the result
mailbox, and posts the requested number of data URLs back to Go. A legacy job
without a variant count defaults to two. AI settings and Reader show worker
connection state; AI settings also expose resolution and variant count.

Verify:

```bash
curl http://127.0.0.1:8787/internal/image-worker/status
```

Expected after at most ~10 seconds:

```json
{
  "connected": true,
  "workerId": "browser-local-1"
}
```

If Perchance asks for Turnstile/CAPTCHA verification, complete it manually. The project contains no bypass logic.

## 9. Play the application

1. Open `http://127.0.0.1:5173`.
2. Create a Story.
3. Generate/review Story Setup.
4. Press Start.
5. Main Timeline / Chapter / Scene / first Beat are committed immediately.
6. Reader opens without waiting for images.
7. An automatic ImageGeneration is created for the committed Scene.
8. Reader shows two skeletons.
9. Violentmonkey claims the job.
10. Perchance starts two parallel 768×768 generations.
11. Go validates/decode/saves both files.
12. Reader polling sees `done` and displays both variants.
13. Choose one image if desired.
14. `Regenerate` creates a new generation rather than overwriting the previous two images.

## 10. Useful diagnostics

The server shares a request gate across all AI/prompt revisions, safe-hybrid
models, setup, Director and illustration-prompt clients. The startup log reports
`hosted_provider_parallelism=3` and `local_provider_parallelism=1`. A waiting
player request precedes a waiting illustration prompt; active calls are allowed
to finish. The Perchance browser image worker is separate from this Story LLM
gate. HTTP 429 introduces cancellable shared backoff with at most three quota
retries per role call. See [request-limit verification](REQUEST_LIMITS_2026-11-10.md).

Backend:

```bash
curl http://127.0.0.1:8787/health/ready
curl http://127.0.0.1:8787/internal/image-worker/status
```

PostgreSQL:

```sql
SELECT id,scene_id,status,attempts,lease_owner,lease_until,error_code
FROM image_generations
ORDER BY created_at DESC;

SELECT generation_id,variant,url
FROM scene_images
ORDER BY created_at DESC;
```

Generated files:

`data/generated/stories/...`

## Full Docker option

`docker compose -f deploy/docker-compose.yml up --build`

also includes a one-shot migration service and persists generated images in a named volume. Host-mode backend is still recommended for the first test because it reaches a loopback llama.cpp server directly. A Docker backend needs the host LLM to be reachable through `host.docker.internal`.

## Current readiness meaning

The project is ready for a real local MVP launch attempt, not for production deployment. The local verification suite covers Docker/PostgreSQL migrations through `00035`, PostgreSQL integration tests, complete Go tests/vet/race checks, frontend tests/lint/typecheck/build, and the production nginx image. A real Story LLM run and an end-to-end Perchance image result through Violentmonkey remain target-machine acceptance gates.
