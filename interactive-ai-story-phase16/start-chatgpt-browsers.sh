#!/usr/bin/env bash
# Visible windows share a dedicated browser profile (login + userscript installed once).
# Each window's worker owns a session UUID. This never clones an existing user profile.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
count="${1:-1}"
if [[ ! "$count" =~ ^[1-9][0-9]*$ ]] || (( ${#count} > 4 )) || (( count > 4096 )); then
  printf '%s\n' 'Использование: ./start-chatgpt-browsers.sh [число окон, 1..4096]' >&2
  exit 2
fi
browser_bin="${CHATGPT_CHROMIUM_BIN:-}"
if [[ -z "$browser_bin" ]]; then
  for candidate in chromium chromium-browser google-chrome; do
    if command -v "$candidate" >/dev/null 2>&1; then browser_bin="$(command -v "$candidate")"; break; fi
  done
fi
if [[ -z "$browser_bin" || ! -x "$browser_bin" ]]; then
  printf '%s\n' 'Не найден Chromium. Установите его обычным способом или задайте CHATGPT_CHROMIUM_BIN.' >&2
  exit 1
fi
profile_dir="$PWD/data/chatgpt-browser-profile"
mkdir -p "$profile_dir" "$PWD/data/run"
chmod 700 "$profile_dir"
for ((i=0;i<count;i++)); do
  "$browser_bin" --user-data-dir="$profile_dir" --new-window 'https://chatgpt.com/?temporary-chat=true#story-bridge-worker' >>"$PWD/data/run/chatgpt-browser.log" 2>&1 &
done
printf '%s\n' "Запрошено окон: $count. Профиль: $profile_dir" 'В этом профиле один раз войдите в ChatGPT и установите Violentmonkey и userscript.' 'В каждом окне нажмите «Запустить» на панели моста. UUID появится на панели.' 'Окна остаются открытыми. Разместите их на втором экране через оконный менеджер.'
