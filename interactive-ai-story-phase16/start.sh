#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
cd "$ROOT"
OPEN_BROWSER=true
case "${1:-}" in
  --no-browser) OPEN_BROWSER=false ;;
  --help|-h)
    printf 'Запуск: ./start.sh [--no-browser]\nОстановка приложения: Ctrl+C. База данных остаётся работать.\n'
    exit 0 ;;
  '') ;;
  *) printf 'Неизвестный параметр. Используй ./start.sh --help\n' >&2; exit 2 ;;
esac

say() { printf '\n%s\n' "$*"; }
die() { printf '\nОшибка: %s\n' "$*" >&2; exit 1; }
for command in docker go node npm python3 make curl setsid flock sha256sum; do
  command -v "$command" >/dev/null || die "Не найдена программа: $command"
done

# Read selected literal values, never execute .env as shell code.
if [[ -f .env ]]; then
  while IFS='=' read -r key value || [[ -n "$key" ]]; do
    case "$key" in
      GENERATION_TURN_PLANNER_V1|GENERATION_PROVISIONAL_BEAT_V1|GENERATION_JOURNAL_EVIDENCE_ISOLATION_V1|GENERATION_FAST_STATE_V1|GENERATION_EXECUTION_TIMEOUT|API_ACCESS_TOKEN|IMAGE_WORKER_TOKEN|HTTP_TRUSTED_NETWORK|HOST_DATABASE_URL|HOST_HTTP_ADDR|HOST_FRONTEND_HOST|HOST_STORY_LLM_BASE_URL|HOST_SECRET_STORE_PATH|HOST_MEDIA_ROOT|HOST_EMBEDDING_BASE_URL|HOST_OMNIVOICE_BASE_URL|IMAGE_WORKER_TOKEN|CHATGPT_BRIDGE_TOKEN_FILE|CHATGPT_BROWSER_PARALLELISM|CHATGPT_BRIDGE_MAX_ACTIVE|CHATGPT_BRIDGE_MAX_PROMPT_KIB|CHATGPT_BRIDGE_INPUT_FORMAT|CHATGPT_BRIDGE_MAX_TEXT_CHARS)
        if [[ ! -v "$key" ]]; then
          value=${value%$'\r'}
          if [[ "$value" == \"*\" || "$value" == \'*\' ]]; then value=${value:1:${#value}-2}; fi
          export "$key=$value"
        fi ;;
    esac
  done < .env
fi

DB_CONTAINER=${STORY_DB_CONTAINER:-deploy-postgres-1}
export HOST_DATABASE_URL=${HOST_DATABASE_URL:-postgres://story:story@127.0.0.1:5432/story?sslmode=disable}
export EMBEDDING_BASE_URL=${HOST_EMBEDDING_BASE_URL:-${EMBEDDING_BASE_URL:-http://127.0.0.1:8090}}
export OMNIVOICE_BASE_URL=${HOST_OMNIVOICE_BASE_URL:-${OMNIVOICE_BASE_URL:-http://127.0.0.1:6655}}
backend_address=${HOST_HTTP_ADDR:-127.0.0.1:8787}
BACKEND_PORT=${STORY_BACKEND_PORT:-${backend_address##*:}}
BACKEND_PORT=${BACKEND_PORT:-8787}
FRONTEND_PORT=${STORY_FRONTEND_PORT:-5173}
URL="http://127.0.0.1:$FRONTEND_PORT"
API="http://127.0.0.1:$BACKEND_PORT"
mkdir -p data/run data/logs data/backups
exec 9>data/run/start.lock
if ! flock -n 9; then
  say "Приложение уже запускается или работает. Открой $URL"
  exit 0
fi
LOG_DIR=$(mktemp -d "$ROOT/data/logs/start-$(date +%Y%m%d-%H%M%S)-XXXXXX")
children=()
cleanup() {
  local status=$?
  trap - EXIT INT TERM HUP
  for pid in "${children[@]}"; do kill -TERM -- "-$pid" 2>/dev/null || true; done
  for ((attempt=0; attempt<10; attempt++)); do
    local alive=false
    for pid in "${children[@]}"; do kill -0 -- "-$pid" 2>/dev/null && alive=true; done
    [[ "$alive" == false ]] && break
    sleep 1
  done
  for pid in "${children[@]}"; do
    kill -KILL -- "-$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  done
  [[ $status == 0 || $status == 130 || $status == 143 ]] || printf 'Логи: %s\n' "$LOG_DIR" >&2
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

mapfile -t DB_INFO < <(python3 - <<'PY'
import os, re
from urllib.parse import urlparse, unquote
try:
    u = urlparse(os.environ['HOST_DATABASE_URL'])
    database, user = unquote(u.path.lstrip('/')), unquote(u.username or '')
    assert u.scheme in ('postgres', 'postgresql') and u.hostname in ('127.0.0.1', 'localhost') and (u.port or 5432) == 5432
    assert all(re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_-]*', v) for v in (database, user))
    print(database)
    print(user)
except (ValueError, AssertionError):
    pass
PY
)
[[ ${#DB_INFO[@]} == 2 ]] || die 'Автозапуск рассчитан на локальную PostgreSQL: localhost:5432.'
DB_NAME=${DB_INFO[0]}
DB_USER=${DB_INFO[1]}
python3 - "$BACKEND_PORT" "$FRONTEND_PORT" <<'PY' || die 'Освободи указанные порты и повтори запуск. Уже работающие приложения не остановлены.'
import socket, sys
ports = sys.argv[1:]
if len(set(ports)) != len(ports):
    sys.exit('Порты backend и интерфейса должны различаться.')
for value in ports:
    try:
        port = int(value)
        if not 1024 <= port <= 65535: raise ValueError()
        with socket.socket() as s:
            s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            s.bind(('127.0.0.1', port))
    except (ValueError, OSError):
        sys.exit(f'Порт {value} занят или некорректен.')
PY

say '1/5 — Запускаю базу данных…'
docker info >/dev/null 2>"$LOG_DIR/docker.log" || die 'Запусти Docker и повтори команду.'
if docker inspect "$DB_CONTAINER" >/dev/null 2>&1; then
  docker start "$DB_CONTAINER" >>"$LOG_DIR/docker.log" 2>&1 || die 'Не удалось запустить PostgreSQL.'
else
  [[ "$DB_CONTAINER" == deploy-postgres-1 ]] || die 'Указанный контейнер PostgreSQL не найден.'
  docker compose -f deploy/docker-compose.yml up -d postgres >>"$LOG_DIR/docker.log" 2>&1 || die 'Не удалось создать PostgreSQL. Подробности в docker.log.'
fi
docker port "$DB_CONTAINER" 5432/tcp | python3 -c 'import sys; sys.exit(0 if any(s.strip() in {"127.0.0.1:5432", "0.0.0.0:5432", "[::]:5432"} for s in sys.stdin) else 1)' || die 'Контейнер должен публиковать PostgreSQL на localhost:5432.'
ready=false
for ((attempt=0; attempt<60; attempt++)); do
  if docker exec "$DB_CONTAINER" pg_isready -U "$DB_USER" -d "$DB_NAME" >>"$LOG_DIR/docker.log" 2>&1; then ready=true; break; fi
  sleep 1
done
[[ "$ready" == true ]] || die 'PostgreSQL не стала доступна за минуту.'
db_query() { docker exec "$DB_CONTAINER" psql -X -v ON_ERROR_STOP=1 -U "$DB_USER" -d "$DB_NAME" -tAc "$1"; }
db_query 'SELECT 1' >"$LOG_DIR/database.log" 2>&1 || die 'Не удалось подключиться к выбранной базе.'

say '2/5 — Проверяю миграции…'
LATEST=$(python3 - <<'PY'
from pathlib import Path
print(max(int(p.name.split('_', 1)[0]) for p in Path('backend/db/migrations').glob('*.sql')))
PY
)
CURRENT=0
VERSION_QUERY='SELECT COALESCE(max(version_id),0) FROM (SELECT DISTINCT ON (version_id) version_id,is_applied FROM goose_db_version ORDER BY version_id,id DESC) versions WHERE is_applied=true'
if [[ -n $(db_query "SELECT to_regclass('public.goose_db_version')" 2>"$LOG_DIR/migrate.log") ]]; then
  CURRENT=$(db_query "$VERSION_QUERY")
fi
[[ "$CURRENT" =~ ^[0-9]+$ ]] || die 'Не удалось определить версию базы.'
(( CURRENT <= LATEST )) || die 'База новее этого кода. Обнови проект перед запуском.'
if (( CURRENT < LATEST )); then
  backup=$(mktemp "$ROOT/data/backups/${DB_NAME}-before-${LATEST}-XXXXXX.dump")
  say "Создаю резервную копию базы: $backup"
  docker exec "$DB_CONTAINER" pg_dump -U "$DB_USER" -d "$DB_NAME" -Fc >"$backup" 2>"$LOG_DIR/backup.log" || die 'Резервное копирование не удалось. Миграции не запущены.'
  docker exec -i "$DB_CONTAINER" pg_restore --list <"$backup" >>"$LOG_DIR/backup.log" 2>&1 || die 'Резервная копия не прошла проверку. Миграции не запущены.'
  make migrate-up >"$LOG_DIR/migrate.log" 2>&1 || die 'Ошибка миграции. Резервная копия сохранена.'
  [[ $(db_query "$VERSION_QUERY") == "$LATEST" ]] || die 'Миграции не достигли нужной версии.'
fi

say '3/5 — Проверяю интерфейс и собираю backend…'
stamp=$(sha256sum frontend/package.json frontend/package-lock.json)
if [[ ! -x frontend/node_modules/.bin/vite || ! -f data/run/frontend-deps.sha256 || $(cat data/run/frontend-deps.sha256) != "$stamp" ]]; then
  npm --prefix frontend ci >"$LOG_DIR/dependencies.log" 2>&1 || die 'Не удалось установить зависимости интерфейса.'
  printf '%s\n' "$stamp" >data/run/frontend-deps.sha256
fi
(cd backend && go build -o "$ROOT/data/run/story-server" ./cmd/server) >"$LOG_DIR/build.log" 2>&1 || die 'Не удалось собрать backend.'

start_process() {
  local log=$1 directory=$2
  shift 2
  (cd "$directory"; exec setsid "$@") >"$LOG_DIR/$log" 2>&1 9>&- &
  children+=("$!")
}
wait_http() {
  local address=$1 pid=$2 seconds=$3
  for ((attempt=0; attempt<seconds; attempt++)); do
    kill -0 "$pid" 2>/dev/null || return 1
    if curl --fail --silent --max-time 2 "$address" >/dev/null; then return 0; fi
    sleep 1
  done
  return 1
}

say '4/5 — Проверяю, нужна ли векторная память…'
memory_enabled=false
if [[ -n $(db_query "SELECT to_regclass('public.ai_active_config')") ]]; then
  memory_enabled=$(db_query "SELECT COALESCE(c.settings->>'memoryEnabled','false') FROM ai_config_revisions c JOIN ai_active_config a ON a.config_revision_id=c.id WHERE a.singleton=true")
fi
if [[ "$memory_enabled" == true ]]; then
  stored_embedding_url=$(db_query "SELECT COALESCE(c.settings->>'embeddingEndpoint','') FROM ai_config_revisions c JOIN ai_active_config a ON a.config_revision_id=c.id WHERE a.singleton=true")
  embedding_url=${stored_embedding_url:-${EMBEDDING_BASE_URL:-http://127.0.0.1:8090}}
  if ! curl --fail --silent --max-time 2 "$embedding_url/health" >/dev/null; then
    [[ "$embedding_url" == http://127.0.0.1:8090 ]] || die 'Сервис памяти по заданному адресу недоступен.'
    if [[ ! -x data/user2-venv/bin/uvicorn ]]; then
      say 'Первый запуск памяти: устанавливаю CPU-зависимости…'
      python3 -m venv data/user2-venv >"$LOG_DIR/memory-install.log" 2>&1 || die 'Не удалось создать окружение памяти.'
      data/user2-venv/bin/pip install --index-url https://download.pytorch.org/whl/cpu 'torch==2.15.0+cpu' >>"$LOG_DIR/memory-install.log" 2>&1 || die 'Не удалось установить CPU PyTorch.'
      data/user2-venv/bin/pip install -r tools/embedding_provider/requirements.txt >>"$LOG_DIR/memory-install.log" 2>&1 || die 'Не удалось установить сервис памяти.'
    fi
    say 'Запускаю USER2. При первом запуске загрузка модели может занять несколько минут…'
    start_process memory.log "$ROOT" env HF_HOME="$ROOT/data/user2-cache" OMP_NUM_THREADS=2 MKL_NUM_THREADS=2 TOKENIZERS_PARALLELISM=false data/user2-venv/bin/uvicorn tools.embedding_provider.app:app --host 127.0.0.1 --port 8090
    wait_http "$embedding_url/health" "${children[-1]}" 300 || die 'Сервис памяти не запустился. Проверь memory.log.'
  fi
  curl --fail --silent --max-time 3 "$embedding_url/health" >"$LOG_DIR/memory-health.json" || die 'Проверка памяти не удалась.'
  python3 - "$LOG_DIR/memory-health.json" <<'PY' || die 'Сервис памяти должен использовать USER2-small с размерностью 384.'
import json, sys
with open(sys.argv[1]) as f: h = json.load(f)
sys.exit(0 if h.get('model') == 'deepvk/USER2-small' and h.get('dimensions') == 384 else 1)
PY
fi

story_provider=$(db_query "SELECT c.story_llm_provider FROM ai_config_revisions c JOIN ai_active_config a ON a.config_revision_id=c.id WHERE a.singleton=true")
if [[ "$story_provider" == chatgpt_browser ]]; then
  bridge_endpoint=$(db_query "SELECT c.settings->>'storyLlmEndpoint' FROM ai_config_revisions c JOIN ai_active_config a ON a.config_revision_id=c.id WHERE a.singleton=true")
  case "$bridge_endpoint" in
    http://127.0.0.1:8791|http://127.0.0.1:8791/|http://127.0.0.1:8791/v1|http://127.0.0.1:8791/v1/chat/completions|http://localhost:8791|http://localhost:8791/|http://localhost:8791/v1|http://localhost:8791/v1/chat/completions)
      if ! curl --fail --silent --max-time 2 http://127.0.0.1:8791/ >/dev/null; then
        say 'Запускаю локальный мост ChatGPT…'
        (cd backend && go build -o "$ROOT/data/run/chatgptbridge" ./cmd/chatgptbridge) >>"$LOG_DIR/build.log" 2>&1 || die 'Не удалось собрать мост ChatGPT.'
        start_process chatgpt-bridge.log "$ROOT" env CHATGPT_BRIDGE_TOKEN_FILE="${CHATGPT_BRIDGE_TOKEN_FILE:-$ROOT/data/secrets/chatgpt-bridge-token}" "$ROOT/data/run/chatgptbridge"
        wait_http http://127.0.0.1:8791/ "${children[-1]}" 30 || die 'Мост ChatGPT не запустился.'
      fi ;;
    *) say 'Для ChatGPT оставь запущенным мост по адресу, сохранённому в настройках ИИ.' ;;
  esac
fi

say '5/5 — Запускаю приложение…'
start_process backend.log "$ROOT/backend" env DATABASE_URL="$HOST_DATABASE_URL" HTTP_ADDR="127.0.0.1:$BACKEND_PORT" STORY_LLM_BASE_URL="${HOST_STORY_LLM_BASE_URL:-http://127.0.0.1:8081}" MEDIA_ROOT="${HOST_MEDIA_ROOT:-../data/generated}" MEDIA_BASE_URL=/media SECRET_STORE_PATH="${HOST_SECRET_STORE_PATH:-../data/secrets/provider-keys.json}" "$ROOT/data/run/story-server"
backend_pid=${children[-1]}
wait_http "$API/health/ready" "$backend_pid" 90 || die 'Backend не запустился. Проверь backend.log.'
start_process frontend.log "$ROOT" env VITE_DEV_BACKEND_TARGET="$API" npm --prefix frontend run dev -- --host "${HOST_FRONTEND_HOST:-127.0.0.1}" --port "$FRONTEND_PORT" --strictPort
frontend_pid=${children[-1]}
wait_http "$URL" "$frontend_pid" 60 || die 'Интерфейс не запустился. Проверь frontend.log.'

say "Готово! Открой $URL"
printf 'Логи: %s\nОстановка: Ctrl+C. База останется работать.\n' "$LOG_DIR"
printf 'Для иллюстраций оставь открытой настроенную вкладку Perchance.\n'
if [[ "$OPEN_BROWSER" == true ]] && command -v xdg-open >/dev/null && [[ -n ${DISPLAY:-}${WAYLAND_DISPLAY:-} ]]; then
  xdg-open "$URL" >>"$LOG_DIR/browser.log" 2>&1 &
fi
while kill -0 "$backend_pid" 2>/dev/null && kill -0 "$frontend_pid" 2>/dev/null; do sleep 2; done
die 'Один из сервисов остановился. Подробности в логах.'
