#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/backend"
export GOCACHE="${GOCACHE:-/tmp/interactive-story-go-cache}"
printf '%s\n' 'Откройте http://127.0.0.1:8791 — инструкции и тесты на странице.' 'Остановка: Ctrl+C. Основная генерация не переключается.'
exec go run ./cmd/chatgptbridge
