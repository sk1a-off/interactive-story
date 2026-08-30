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

Migrations currently run through `00021_perchance_prompt_profile_v2.sql`.

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

To open the application from a phone connected through WireGuard, bind the backend to all interfaces:

```bash
HOST_HTTP_ADDR='0.0.0.0:8787' make backend-dev
```

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
npm install
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

For WireGuard/mobile access:

```bash
cd frontend
npm run dev -- --host 0.0.0.0
```

Then open `http://10.10.10.2:5173/` on the phone.

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
- square 768×768
- two `generateImage()` calls in `Promise.all`

It receives a scene prompt, a frozen style layer, and a separate negative layer. Do not embed `(resolution:::)`, `(seed:::)`, `(guidanceScale:::)`, or `(negativePrompt:::)` tokens in story prompts; the backend strips them and the worker owns validated provider options.

Do not add localhost fetch logic to Perchance.

## 8. Install Violentmonkey transport

Create a new Violentmonkey script and paste:

`tools/perchance/violentmonkey.user.js`

Save/enable it and keep the Perchance generator tab open.

It polls:

`http://127.0.0.1:8787/internal/image-worker/jobs/next`

using `GM_xmlhttpRequest`, writes the job to the DOM mailbox, reads the result mailbox, and posts the two data URLs back to Go.

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

The project is ready for a real local MVP launch attempt, not for production deployment. The 2026-08-23 audit verified Docker/PostgreSQL migrations through `00021`, PostgreSQL integration tests, complete Go tests/vet/build, frontend tests/lint/typecheck/build, and read-only inspection of the live Perchance generator. A real GGUF story run and an end-to-end Perchance image result through Violentmonkey remain target-machine acceptance gates.
