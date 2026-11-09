#!/usr/bin/env bash
set -Eeuo pipefail

# Real-game Turn Planner canary for feature-1.
#
# One command:
#   chmod +x feature1-planner-real-canary.sh
#   ./feature1-planner-real-canary.sh
#
# Then use the web UI normally and complete CANARY_TURNS successful story turns.
# The script watches PostgreSQL and stops automatically after the target count.
#
# It DOES NOT modify Git.
# It temporarily recreates only the Docker Compose backend with:
#   GENERATION_TURN_PLANNER_V1=true
# and ALWAYS recreates it with the flag=false on exit.
#
# Output:
#   interactive-ai-story-phase16/artifacts/feature1-planner-real-canary-*.zip
#
# Optional:
#   CANARY_TURNS=5            (default 5)
#   CANARY_TIMEOUT_MIN=90     (default 90)
#   BASELINE_JOBS=10          (default 10)
#   DATABASE_URL=postgres://story:story@127.0.0.1:5432/story?sslmode=disable

CANARY_TURNS="${CANARY_TURNS:-5}"
CANARY_TIMEOUT_MIN="${CANARY_TIMEOUT_MIN:-90}"
BASELINE_JOBS="${BASELINE_JOBS:-10}"
STAMP="$(date +%Y%m%d-%H%M%S)"

say() { printf '\n[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }
pass() { printf '%-38s PASS\n' "$1"; }
fail() { printf '%-38s FAILED\n' "$1" >&2; }

ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
[[ -n "$ROOT" ]] || {
  echo "ERROR: run from inside the interactive-story repository." >&2
  exit 2
}

BRANCH="$(git -C "$ROOT" branch --show-current)"
[[ "$BRANCH" == "feature-1" ]] || {
  echo "ERROR: expected feature-1, current branch is '$BRANCH'." >&2
  exit 2
}

WORKSPACE="$ROOT/interactive-ai-story-phase16"
BACKEND="$WORKSPACE/backend"
COMPOSE="$WORKSPACE/deploy/docker-compose.yml"

[[ -f "$COMPOSE" ]] || {
  echo "ERROR: compose file missing: $COMPOSE" >&2
  exit 2
}

for cmd in git docker python3; do
  command -v "$cmd" >/dev/null 2>&1 || {
    echo "ERROR: missing command: $cmd" >&2
    exit 2
  }
done
docker compose version >/dev/null 2>&1 || {
  echo "ERROR: docker compose plugin unavailable." >&2
  exit 2
}

case "$CANARY_TURNS" in
  ''|*[!0-9]*) echo "CANARY_TURNS must be a positive integer." >&2; exit 2 ;;
esac
case "$CANARY_TIMEOUT_MIN" in
  ''|*[!0-9]*) echo "CANARY_TIMEOUT_MIN must be a positive integer." >&2; exit 2 ;;
esac
(( CANARY_TURNS >= 1 )) || exit 2
(( CANARY_TIMEOUT_MIN >= 1 )) || exit 2

RESULT_ROOT="$WORKSPACE/artifacts/feature1-planner-real-canary-v3-$STAMP"
mkdir -p "$RESULT_ROOT"
SUMMARY="$RESULT_ROOT/SUMMARY.txt"
: >"$SUMMARY"

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/feature1-planner-real-canary.XXXXXX")"
OVERRIDE="$TMP_DIR/planner-canary.override.yml"
RESTORE_NEEDED=0
CANARY_STARTED=0
PACKAGED=0

compose_canary() {
  docker compose -f "$COMPOSE" -f "$OVERRIDE" "$@"
}

write_override() {
  local enabled="$1"

  if [[ "$SECRET_MOUNT_TYPE" == "volume" ]]; then
    cat >"$OVERRIDE" <<YAML
services:
  backend:
    environment:
      GENERATION_TURN_PLANNER_V1: "$enabled"
    volumes:
      - type: volume
        source: canary-provider-secrets
        target: /data/secrets
volumes:
  canary-provider-secrets:
    external: true
    name: "$SECRET_MOUNT_SOURCE"
YAML
  elif [[ "$SECRET_MOUNT_TYPE" == "bind" ]]; then
    cat >"$OVERRIDE" <<YAML
services:
  backend:
    environment:
      GENERATION_TURN_PLANNER_V1: "$enabled"
    volumes:
      - type: bind
        source: "$SECRET_MOUNT_SOURCE"
        target: /data/secrets
YAML
  else
    echo "ERROR: unsupported secret mount type '$SECRET_MOUNT_TYPE'." >&2
    return 1
  fi
}

restore_planner_off() {
  if [[ "$RESTORE_NEEDED" != "1" ]]; then
    return 0
  fi
  echo
  echo "Restoring backend with Turn Planner OFF and the verified existing secret store..."
  write_override "false"
  set +e
  compose_canary up -d --no-deps --force-recreate backend \
    >"$RESULT_ROOT/backend-restore.log" 2>&1
  local rc=$?
  set -e
  if [[ "$rc" == "0" ]]; then
    echo "Backend restored with GENERATION_TURN_PLANNER_V1=false and the existing credentials mounted."
  else
    echo "WARNING: automatic backend restore failed." >&2
    echo "Run manually:" >&2
    echo "  docker compose -f '$COMPOSE' up -d --force-recreate backend" >&2
  fi
  RESTORE_NEEDED=0
}

cleanup() {
  local rc=$?
  restore_planner_off || true
  rm -rf "$TMP_DIR" >/dev/null 2>&1 || true
  exit "$rc"
}
trap cleanup EXIT INT TERM

{
  echo "feature-1 real Planner canary v3"
  echo "started_local=$(date --iso-8601=seconds 2>/dev/null || date)"
  echo "branch=$BRANCH"
  echo "head=$(git -C "$ROOT" rev-parse HEAD)"
  echo "canary_turns=$CANARY_TURNS"
  echo "timeout_minutes=$CANARY_TIMEOUT_MIN"
  echo
} >>"$SUMMARY"

###############################################################################
# 1. Verify canary code / repository state
###############################################################################
say "Checking Planner canary code"

grep -q 'GENERATION_TURN_PLANNER_V1' "$BACKEND/internal/config/config.go" || {
  fail "Planner feature flag"
  echo "Apply the Planner canary patch first." >&2
  exit 3
}
grep -q 'if cfg.GenerationTurnPlanner' "$BACKEND/cmd/server/main.go" || {
  fail "Planner server wiring"
  exit 3
}
grep -q '"turn_planner"' "$BACKEND/internal/adapters/storyllm/role_router/router.go" || {
  fail "Planner fast-role routing"
  exit 3
}
grep -q 'IntentFallbackUsed' "$BACKEND/internal/application/generation/turn_planner.go" || {
  fail "Planner agency fallback"
  exit 3
}
pass "Planner canary code"

{
  echo "=== git status ==="
  git -C "$ROOT" status --short
  echo
  echo "=== unstaged stat ==="
  git -C "$ROOT" diff --stat
  echo
  echo "=== staged stat ==="
  git -C "$ROOT" diff --cached --stat
} >"$RESULT_ROOT/git-state.txt"

git -C "$ROOT" diff >"$RESULT_ROOT/production-working-tree.patch"
git -C "$ROOT" diff --cached >"$RESULT_ROOT/staged.patch"

###############################################################################
# 2. PostgreSQL connectivity
###############################################################################
say "Detecting PostgreSQL"

DATABASE_URL="${DATABASE_URL:-postgres://story:story@127.0.0.1:5432/story?sslmode=disable}"
DB_MODE=""

db_host() {
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -Atq -c "$1"
}

db_docker() {
  printf '%s\n' "$1" | docker compose -f "$COMPOSE" exec -T postgres \
    sh -lc 'psql -v ON_ERROR_STOP=1 -U "${POSTGRES_USER:-story}" -d "${POSTGRES_DB:-story}" -Atq'
}

if command -v psql >/dev/null 2>&1 && db_host 'SELECT 1;' >/dev/null 2>&1; then
  DB_MODE="host-psql"
  db_query() { db_host "$1"; }
elif db_docker 'SELECT 1;' >/dev/null 2>&1; then
  DB_MODE="docker-compose-psql"
  db_query() { db_docker "$1"; }
else
  fail "PostgreSQL"
  echo "Could not connect through host psql or docker compose postgres." >&2
  exit 4
fi
pass "PostgreSQL ($DB_MODE)"

# Capture schema for later forensic analysis.
db_query "
SELECT table_name || E'\t' || column_name || E'\t' || data_type
FROM information_schema.columns
WHERE table_schema='public'
  AND table_name IN (
    'generation_jobs',
    'generation_run_metrics',
    'generation_attempts',
    'generation_updates',
    'story_events'
  )
ORDER BY table_name, ordinal_position;
" >"$RESULT_ROOT/relevant-schema.tsv"

###############################################################################
# 3. Verify Safe Hybrid, because Planner must route to Flash-Lite
###############################################################################
say "Checking active AI configuration"

ACTIVE_CONFIG="$(db_query "
SELECT
  c.id || E'\t' ||
  c.revision || E'\t' ||
  c.story_llm_provider || E'\t' ||
  c.story_llm_model || E'\t' ||
  c.story_llm_profile || E'\t' ||
  COALESCE(c.settings->>'storyLlmSafeHybrid','false')
FROM ai_config_revisions c
JOIN ai_active_config a ON a.config_revision_id=c.id
WHERE a.singleton=true;
")"

printf '%s\n' "$ACTIVE_CONFIG" >"$RESULT_ROOT/active-ai-config.tsv"

IFS=$'\t' read -r CONFIG_ID CONFIG_REV PROVIDER MODEL PROFILE SAFE_HYBRID <<<"$ACTIVE_CONFIG"

if [[ "${SAFE_HYBRID,,}" != "true" ]]; then
  fail "Safe Hybrid active"
  echo
  echo "The Planner canary must use Safe Hybrid so turn_planner is routed to"
  echo "gemini-3.5-flash-lite while writer/evaluators remain on quality."
  echo "Current storyLlmSafeHybrid=$SAFE_HYBRID"
  echo
  echo "Enable Safe Hybrid in the application's AI settings, then rerun this script."
  exit 5
fi
pass "Safe Hybrid active"
echo "Active story model: $MODEL"

###############################################################################
# 3b. Find the EXISTING secret store that contains the active revision key.
#     Prefer the currently mounted backend store, then scan Docker volumes,
#     then optional/known bind paths. Never print secret values.
###############################################################################
say "Locating existing provider secret store"

SECRET_MOUNT_TYPE=""
SECRET_MOUNT_SOURCE=""
SECRET_DISCOVERY_NOTE=""

host_file_has_active_key() {
  local file="$1"
  [[ -s "$file" ]] || return 1
  python3 - "$file" "$CONFIG_ID" <<'PYSECRET' >/dev/null
import json, sys
from pathlib import Path
path = Path(sys.argv[1])
config_id = sys.argv[2]
try:
    data = json.loads(path.read_text(encoding="utf-8"))
except Exception:
    raise SystemExit(1)
if not isinstance(data, dict):
    raise SystemExit(1)
value = data.get("story_llm_api_key:" + config_id)
raise SystemExit(0 if isinstance(value, str) and value.strip() else 1)
PYSECRET
}

volume_has_active_key() {
  local volume="$1"
  docker run --rm \
    -e ACTIVE_CONFIG_ID="$CONFIG_ID" \
    -v "$volume:/secrets:ro" \
    alpine:3.21 sh -lc \
    'test -s /secrets/provider-keys.json &&
     grep -Fq "\"story_llm_api_key:${ACTIVE_CONFIG_ID}\"" /secrets/provider-keys.json' \
    >/dev/null 2>&1
}

# 1) Prefer the store mounted into the currently running Compose backend.
CURRENT_BACKEND="$(docker compose -f "$COMPOSE" ps -q backend 2>/dev/null || true)"
if [[ -n "$CURRENT_BACKEND" ]]; then
  CURRENT_SECRET_MOUNT="$(docker inspect -f \
    '{{range .Mounts}}{{if eq .Destination "/data/secrets"}}{{.Type}}{{printf "\t"}}{{.Name}}{{printf "\t"}}{{.Source}}{{end}}{{end}}' \
    "$CURRENT_BACKEND" 2>/dev/null || true)"

  if [[ -n "$CURRENT_SECRET_MOUNT" ]]; then
    IFS=$'\t' read -r cur_type cur_name cur_source <<<"$CURRENT_SECRET_MOUNT"
    if [[ "$cur_type" == "volume" && -n "$cur_name" ]] && volume_has_active_key "$cur_name"; then
      SECRET_MOUNT_TYPE="volume"
      SECRET_MOUNT_SOURCE="$cur_name"
      SECRET_DISCOVERY_NOTE="current backend Docker volume"
    elif [[ "$cur_type" == "bind" && -n "$cur_source" ]] && host_file_has_active_key "$cur_source/provider-keys.json"; then
      SECRET_MOUNT_TYPE="bind"
      SECRET_MOUNT_SOURCE="$(readlink -f "$cur_source")"
      SECRET_DISCOVERY_NOTE="current backend bind mount"
    fi
  fi
fi

# 2) Scan Docker volumes. The original project normally stores provider keys
#    in a named volume, and previous Compose project names may have created
#    more than one provider-secrets volume.
if [[ -z "$SECRET_MOUNT_TYPE" ]]; then
  while IFS= read -r volume; do
    [[ -n "$volume" ]] || continue
    if volume_has_active_key "$volume"; then
      SECRET_MOUNT_TYPE="volume"
      SECRET_MOUNT_SOURCE="$volume"
      SECRET_DISCOVERY_NOTE="Docker volume scan"
      break
    fi
  done < <(
    {
      docker volume ls --format '{{.Name}}' 2>/dev/null | grep -E 'provider[-_]secrets|secrets' || true
      docker volume ls --format '{{.Name}}' 2>/dev/null || true
    } | awk '!seen[$0]++'
  )
fi

# 3) Explicit/known host stores remain supported, but are not assumed.
if [[ -z "$SECRET_MOUNT_TYPE" ]]; then
  HOST_SECRET_CANDIDATES=()
  if [[ -n "${SECRET_STORE:-}" ]]; then
    HOST_SECRET_CANDIDATES+=("$SECRET_STORE")
  fi
  HOST_SECRET_CANDIDATES+=(
    "$WORKSPACE/data/secrets/provider-keys.json"
    "$ROOT/data/secrets/provider-keys.json"
    "$HOME/Projects/interactive-ai-story-phase16-result/interactive-ai-story-phase16/data/secrets/provider-keys.json"
  )

  for candidate in "${HOST_SECRET_CANDIDATES[@]}"; do
    if host_file_has_active_key "$candidate"; then
      SECRET_MOUNT_TYPE="bind"
      SECRET_MOUNT_SOURCE="$(dirname "$(readlink -f "$candidate")")"
      SECRET_DISCOVERY_NOTE="host file scan"
      break
    fi
  done
fi

if [[ -z "$SECRET_MOUNT_TYPE" ]]; then
  fail "Active revision credential store"
  echo
  echo "No existing Docker volume or host provider-keys.json contains a credential"
  echo "for active AI revision: $CONFIG_ID"
  echo
  echo "Nothing was restarted and Planner was not enabled."
  echo "Do NOT create a new revision just for this test."
  exit 5
fi

pass "Active revision credential store"
echo "Secret source type: $SECRET_MOUNT_TYPE"
echo "Secret source: $SECRET_MOUNT_SOURCE"
echo "Discovered via: $SECRET_DISCOVERY_NOTE"
echo "Active revision credential: present (value not displayed)"

###############################################################################
# 4. Refuse to restart while another generation is running
###############################################################################
RUNNING_JOBS="$(db_query "SELECT count(*) FROM generation_jobs WHERE status='running';" | tr -d '[:space:]')"
if [[ "$RUNNING_JOBS" != "0" ]]; then
  fail "No active story generation"
  echo "There are $RUNNING_JOBS running generation jobs. Wait until they finish and rerun." >&2
  exit 6
fi
pass "No active story generation"

###############################################################################
# 5. Capture baseline before canary
###############################################################################
say "Capturing recent baseline"

BASELINE_CUTOFF="$(db_query "SELECT to_char(clock_timestamp() AT TIME ZONE 'UTC','YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"');" | tr -d '\r\n')"

db_query "
WITH selected AS (
  SELECT j.id
  FROM generation_jobs j
  WHERE j.created_at < '$BASELINE_CUTOFF'::timestamptz
    AND EXISTS (
      SELECT 1 FROM generation_run_metrics m
      WHERE m.generation_job_id=j.id AND m.succeeded=true
    )
  ORDER BY j.created_at DESC
  LIMIT $BASELINE_JOBS
)
SELECT jsonb_build_object(
  'job', to_jsonb(j),
  'metrics', COALESCE((
    SELECT jsonb_agg(to_jsonb(m) ORDER BY m.attempt_no)
    FROM generation_run_metrics m
    WHERE m.generation_job_id=j.id
  ), '[]'::jsonb),
  'attempts', COALESCE((
    SELECT jsonb_agg(to_jsonb(a) ORDER BY a.attempt_no,a.call_sequence)
    FROM generation_attempts a
    WHERE a.generation_job_id=j.id
  ), '[]'::jsonb)
)
FROM selected s
JOIN generation_jobs j ON j.id=s.id
ORDER BY j.created_at;
" >"$RESULT_ROOT/baseline-jobs.jsonl"

###############################################################################
# 6. Build/restart backend with Planner ON
###############################################################################
say "Starting backend with Turn Planner ON"

write_override "true"
RESTORE_NEEDED=1

set +e
docker compose -f "$COMPOSE" build backend \
  > >(tee "$RESULT_ROOT/backend-build.log") \
  2> >(tee -a "$RESULT_ROOT/backend-build.log" >&2)
BUILD_RC=$?
set -e
if [[ "$BUILD_RC" != "0" ]]; then
  fail "Backend image build"
  exit 7
fi
pass "Backend image build"

set +e
compose_canary up -d --no-deps --force-recreate backend \
  > >(tee "$RESULT_ROOT/backend-enable.log") \
  2> >(tee -a "$RESULT_ROOT/backend-enable.log" >&2)
ENABLE_RC=$?
set -e

if [[ "$ENABLE_RC" != "0" ]]; then
  fail "Backend canary start"
  exit 7
fi

# Wait until the container is running and verify the actual environment.
READY=0
for _ in $(seq 1 60); do
  if compose_canary ps --status running backend 2>/dev/null | grep -q backend; then
    READY=1
    break
  fi
  sleep 2
done
if [[ "$READY" != "1" ]]; then
  compose_canary logs --tail=150 backend >"$RESULT_ROOT/backend-start-failure.log" 2>&1 || true
  fail "Backend running"
  exit 7
fi
pass "Backend running"

ACTUAL_FLAG="$(compose_canary exec -T backend sh -lc 'printf "%s" "${GENERATION_TURN_PLANNER_V1:-}"' 2>/dev/null || true)"
if [[ "${ACTUAL_FLAG,,}" != "true" ]]; then
  fail "Planner env inside backend"
  echo "Container has GENERATION_TURN_PLANNER_V1='$ACTUAL_FLAG'." >&2
  exit 7
fi
pass "Planner env inside backend"

CONTAINER_ID="$(compose_canary ps -q backend)"
MOUNT_INFO="$(docker inspect -f \
  '{{range .Mounts}}{{if eq .Destination "/data/secrets"}}{{.Type}}{{printf "\t"}}{{.Name}}{{printf "\t"}}{{.Source}}{{end}}{{end}}' \
  "$CONTAINER_ID")"
IFS=$'\t' read -r MOUNT_TYPE MOUNT_NAME MOUNT_SOURCE <<<"$MOUNT_INFO"

if [[ "$SECRET_MOUNT_TYPE" == "volume" ]]; then
  if [[ "$MOUNT_TYPE" != "volume" || "$MOUNT_NAME" != "$SECRET_MOUNT_SOURCE" ]]; then
    fail "Verified existing secret volume"
    echo "Expected Docker volume: $SECRET_MOUNT_SOURCE" >&2
    echo "Actual mount: $MOUNT_INFO" >&2
    exit 7
  fi
else
  if [[ "$MOUNT_TYPE" != "bind" || "$(readlink -f "$MOUNT_SOURCE")" != "$SECRET_MOUNT_SOURCE" ]]; then
    fail "Verified existing secret bind"
    echo "Expected bind source: $SECRET_MOUNT_SOURCE" >&2
    echo "Actual mount: $MOUNT_INFO" >&2
    exit 7
  fi
fi
pass "Verified existing secret mount"

if ! compose_canary exec -T backend sh -lc 'test -s /data/secrets/provider-keys.json'; then
  fail "Secret file visible in backend"
  exit 7
fi
pass "Secret file visible in backend"

if ! compose_canary exec -T -e ACTIVE_CONFIG_ID="$CONFIG_ID" backend sh -lc \
  'grep -Fq "\"story_llm_api_key:${ACTIVE_CONFIG_ID}\"" /data/secrets/provider-keys.json'; then
  fail "Active revision credential in backend"
  exit 7
fi
pass "Active revision credential in backend"

# Reset the canary marker only after the backend and credential mount are verified,
# so the failed run from the previous script cannot contaminate this result.
START_DB="$(db_query "SELECT to_char(clock_timestamp() AT TIME ZONE 'UTC','YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"');" | tr -d '\r\n')"
echo "$START_DB" >"$RESULT_ROOT/canary-start-utc.txt"

CANARY_STARTED=1

###############################################################################
# 7. Watch real gameplay
###############################################################################
say "CANARY IS LIVE"

echo
echo "Open/use the application normally."
echo
echo "Complete $CANARY_TURNS successful story turns."
echo
echo "Wait for each turn to finish before choosing the next action."
echo
echo
echo "This script will detect completed turns automatically."
echo "You do NOT need to press Enter."
echo

START_EPOCH="$(date +%s)"
TIMEOUT_SEC="$((CANARY_TIMEOUT_MIN * 60))"
LAST_LINE=""

while true; do
  SUCCESSFUL="$(db_query "
SELECT count(DISTINCT j.id)
FROM generation_jobs j
WHERE j.created_at >= '$START_DB'::timestamptz
  AND EXISTS (
    SELECT 1 FROM generation_run_metrics m
    WHERE m.generation_job_id=j.id AND m.succeeded=true
  );
" | tr -d '[:space:]')"

  TOTAL_NEW="$(db_query "
SELECT count(*)
FROM generation_jobs
WHERE created_at >= '$START_DB'::timestamptz;
" | tr -d '[:space:]')"

  FAILED_NEW="$(db_query "
SELECT count(*)
FROM generation_jobs
WHERE created_at >= '$START_DB'::timestamptz
  AND status IN ('failed','cancelled');
" | tr -d '[:space:]')"

  RUNNING_NEW="$(db_query "
SELECT count(*)
FROM generation_jobs
WHERE created_at >= '$START_DB'::timestamptz
  AND status IN ('queued','running');
" | tr -d '[:space:]')"

  LINE="successful=$SUCCESSFUL/$CANARY_TURNS total=$TOTAL_NEW active=$RUNNING_NEW failed/cancelled=$FAILED_NEW"
  if [[ "$LINE" != "$LAST_LINE" ]]; then
    printf '[%s] %s\n' "$(date +%H:%M:%S)" "$LINE"
    LAST_LINE="$LINE"
  fi

  if (( SUCCESSFUL >= CANARY_TURNS )); then
    break
  fi

  NOW="$(date +%s)"
  if (( NOW - START_EPOCH >= TIMEOUT_SEC )); then
    echo
    echo "Canary timeout reached after ${CANARY_TIMEOUT_MIN} minutes."
    echo "Partial results will still be collected."
    break
  fi

  sleep 5
done

END_DB="$(db_query "SELECT to_char(clock_timestamp() AT TIME ZONE 'UTC','YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"');" | tr -d '\r\n')"
echo "$END_DB" >"$RESULT_ROOT/canary-end-utc.txt"

###############################################################################
# 8. Capture canary jobs + attempts + durable events + updates
###############################################################################
say "Collecting canary evidence"

db_query "
SELECT jsonb_build_object(
  'job', to_jsonb(j),
  'metrics', COALESCE((
    SELECT jsonb_agg(to_jsonb(m) ORDER BY m.attempt_no)
    FROM generation_run_metrics m
    WHERE m.generation_job_id=j.id
  ), '[]'::jsonb),
  'attempts', COALESCE((
    SELECT jsonb_agg(to_jsonb(a) ORDER BY a.attempt_no,a.call_sequence)
    FROM generation_attempts a
    WHERE a.generation_job_id=j.id
  ), '[]'::jsonb),
  'events', COALESCE((
    SELECT jsonb_agg(to_jsonb(e) ORDER BY e.seq)
    FROM story_events e
    WHERE e.generation_id=j.id
  ), '[]'::jsonb),
  'updates', COALESCE((
    SELECT jsonb_agg(to_jsonb(u) ORDER BY u.sequence)
    FROM generation_updates u
    WHERE u.generation_job_id=j.id
  ), '[]'::jsonb)
)
FROM generation_jobs j
WHERE j.created_at >= '$START_DB'::timestamptz
  AND j.created_at <= '$END_DB'::timestamptz
ORDER BY j.created_at;
" >"$RESULT_ROOT/canary-jobs.jsonl"

# Capture server logs before turning the canary off.
compose_canary logs --since "$START_DB" backend \
  >"$RESULT_ROOT/backend-canary.log" 2>&1 || \
compose_canary logs --tail=1000 backend \
  >"$RESULT_ROOT/backend-canary.log" 2>&1 || true

###############################################################################
# 9. Summarize real canary and compare with recent baseline
###############################################################################
python3 - "$RESULT_ROOT/baseline-jobs.jsonl" "$RESULT_ROOT/canary-jobs.jsonl" \
  "$RESULT_ROOT/canary-summary.json" "$RESULT_ROOT/canary-per-job.tsv" <<'PY'
import json
import statistics
import sys
from pathlib import Path

baseline_path, canary_path, summary_path, tsv_path = map(Path, sys.argv[1:])

def read_jsonl(path):
    rows = []
    if not path.exists():
        return rows
    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            rows.append(json.loads(line))
        except Exception as exc:
            rows.append({"_parse_error": str(exc), "_raw": line[:1000]})
    return rows

def final_metric(row):
    metrics = row.get("metrics") or []
    successful = [m for m in metrics if m.get("succeeded")]
    if successful:
        return successful[-1]
    return metrics[-1] if metrics else {}

def p50(vals):
    vals = sorted(int(v or 0) for v in vals)
    if not vals:
        return 0
    return vals[(len(vals)-1)//2]

def p95(vals):
    vals = sorted(int(v or 0) for v in vals)
    if not vals:
        return 0
    idx = max(0, min(len(vals)-1, (95*len(vals)+99)//100 - 1))
    return vals[idx]

def set_summary(rows):
    usable = [r for r in rows if "_parse_error" not in r]
    metrics = [final_metric(r) for r in usable]
    metrics = [m for m in metrics if m]

    jobs_success = sum(bool(m.get("succeeded")) for m in metrics)
    total = [m.get("total_ms",0) for m in metrics if m.get("succeeded")]
    ttft = [m.get("time_to_first_story_text_ms",0) for m in metrics if m.get("succeeded")]
    planner = [m.get("planner_ms",0) for m in metrics if m.get("succeeded")]
    writer = [m.get("writer_ms",0) for m in metrics if m.get("succeeded")]
    post = [m.get("post_writer_ms",0) for m in metrics if m.get("succeeded")]

    attempts = []
    roles = {}
    turn_planner_models = {}
    for row in usable:
        for call in row.get("attempts") or []:
            attempts.append(call)
            role = str(call.get("role") or "")
            model = str(call.get("model_name") or "")
            roles[role] = roles.get(role, 0) + 1
            if role == "turn_planner":
                turn_planner_models[model] = turn_planner_models.get(model, 0) + 1

    return {
        "jobs": len(usable),
        "successfulJobs": jobs_success,
        "p50TotalMs": p50(total),
        "p95TotalMs": p95(total),
        "p50TTFTMs": p50(ttft),
        "p95TTFTMs": p95(ttft),
        "p50PlannerMs": p50(planner),
        "p50WriterMs": p50(writer),
        "p50PostWriterMs": p50(post),
        "actionIntentFallbacks": sum(bool(m.get("action_intent_fallback_used")) for m in metrics),
        "choiceFallbacks": sum(bool(m.get("choices_fallback_used")) for m in metrics),
        "llmCalls": len(attempts),
        "roleCalls": roles,
        "turnPlannerModels": turn_planner_models,
    }

baseline = read_jsonl(baseline_path)
canary = read_jsonl(canary_path)

b = set_summary(baseline)
c = set_summary(canary)

comparison = {}
for key in ("p50TotalMs","p50TTFTMs","p50PlannerMs","p50WriterMs","p50PostWriterMs"):
    bv = b.get(key,0) or 0
    cv = c.get(key,0) or 0
    comparison[key+"DeltaPct"] = round((cv-bv)*100/max(1,bv), 1)

report = {
    "baseline": b,
    "canary": c,
    "comparison": comparison,
}
summary_path.write_text(json.dumps(report, ensure_ascii=False, indent=2)+"\n", encoding="utf-8")

cols = [
    "jobId","status","action","attemptNo","succeeded","totalMs","ttftMs","plannerMs",
    "writerMs","postWriterMs","intentFallback","choiceFallback","turnPlannerModel",
    "turnPlannerLatencyMs","eventTypes"
]
with tsv_path.open("w", encoding="utf-8") as f:
    f.write("\t".join(cols)+"\n")
    for row in canary:
        if "_parse_error" in row:
            continue
        job = row.get("job") or {}
        metric = final_metric(row)
        payload = job.get("request_payload")
        action = ""
        if isinstance(payload, dict):
            action = str(payload.get("text") or "")
        elif isinstance(payload, str):
            try:
                parsed = json.loads(payload)
                action = str(parsed.get("text") or "")
            except Exception:
                pass

        planner_calls = [a for a in (row.get("attempts") or []) if a.get("role")=="turn_planner"]
        planner_model = planner_calls[-1].get("model_name","") if planner_calls else ""
        planner_latency = planner_calls[-1].get("latency_ms",0) if planner_calls else 0
        event_types = ",".join(str(e.get("event_type") or "") for e in (row.get("events") or []))

        vals = [
            job.get("id",""), job.get("status",""), action.replace("\t"," ").replace("\n"," "),
            metric.get("attempt_no",""), metric.get("succeeded",""),
            metric.get("total_ms",0), metric.get("time_to_first_story_text_ms",0),
            metric.get("planner_ms",0), metric.get("writer_ms",0),
            metric.get("post_writer_ms",0), metric.get("action_intent_fallback_used",False),
            metric.get("choices_fallback_used",False), planner_model, planner_latency, event_types
        ]
        f.write("\t".join(str(v) for v in vals)+"\n")

print(json.dumps(report, ensure_ascii=False, indent=2))
PY

###############################################################################
# 10. Restore Planner OFF before packaging
###############################################################################
say "Turning Planner OFF again"
restore_planner_off

# Verify actual env in restored container if it is running.
set +e
RESTORED_FLAG="$(compose_canary exec -T backend sh -lc 'printf "%s" "${GENERATION_TURN_PLANNER_V1:-}"' 2>/dev/null)"
VERIFY_RC=$?
set -e
{
  echo "restore_exec_rc=$VERIFY_RC"
  echo "GENERATION_TURN_PLANNER_V1=$RESTORED_FLAG"
} >"$RESULT_ROOT/restore-verification.txt"

if [[ "$VERIFY_RC" == "0" && "${RESTORED_FLAG,,}" != "true" ]]; then
  pass "Planner restored OFF"
else
  echo "WARNING: could not conclusively verify restored Planner flag." >&2
fi

###############################################################################
# 11. Package
###############################################################################
say "Packaging result"

{
  echo
  echo "canary_start_utc=$START_DB"
  echo "canary_end_utc=$END_DB"
  echo "db_mode=$DB_MODE"
  echo "active_config_id=$CONFIG_ID"
  echo "active_config_revision=$CONFIG_REV"
  echo "safe_hybrid=$SAFE_HYBRID"
  echo "canary_target_turns=$CANARY_TURNS"
  echo "finished_local=$(date --iso-8601=seconds 2>/dev/null || date)"
} >>"$SUMMARY"

cp "$0" "$RESULT_ROOT/runner-used.sh" 2>/dev/null || true

ZIP="$WORKSPACE/artifacts/feature1-planner-real-canary-$STAMP.zip"
python3 - "$RESULT_ROOT" "$ZIP" <<'PY'
from pathlib import Path
import sys
import zipfile

root = Path(sys.argv[1])
dest = Path(sys.argv[2])

with zipfile.ZipFile(dest, "w", zipfile.ZIP_DEFLATED) as z:
    for p in sorted(root.rglob("*")):
        if p.is_file():
            z.write(p, arcname=p.relative_to(root.parent))

print(dest)
PY

PACKAGED=1

echo
echo "============================================================"
echo "DONE"
echo
echo "Turn Planner has been restored OFF."
echo "Send me this ONE file:"
echo
echo "  $ZIP"
echo "============================================================"
