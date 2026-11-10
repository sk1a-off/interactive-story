#!/usr/bin/env bash
set -euo pipefail
bridge_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
export CHATGPT_BRIDGE_TOKEN_FILE="${CHATGPT_BRIDGE_TOKEN_FILE:-$bridge_root/data/secrets/chatgpt-bridge-token}"
cd -- "$bridge_root/backend"
export GOCACHE="${GOCACHE:-/tmp/interactive-story-go-cache}"
printf '%s\n' 'Откройте http://127.0.0.1:8791 — инструкции и тесты на странице.' 'Остановка: Ctrl+C. Основная генерация не переключается.'
exec go run ./cmd/chatgptbridge
