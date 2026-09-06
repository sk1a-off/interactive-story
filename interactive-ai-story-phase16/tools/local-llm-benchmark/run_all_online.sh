#!/usr/bin/env bash
set -uo pipefail

workspace=/home/sk1a/Projects/interactive-ai-story-phase16-result/interactive-ai-story-phase16
backend_dir="$workspace/backend"
results_dir="$workspace/tools/local-llm-benchmark/results"
secret_store="$workspace/data/secrets/provider-keys.json"
secret_name='story_llm_api_key:c1f554ea-0375-41ed-a141-e4d4b23d4601'
chat_endpoint='https://generativelanguage.googleapis.com/v1beta/openai/chat/completions'
interactions_endpoint='https://generativelanguage.googleapis.com/v1beta/interactions'

models=(
  'gemini-3.6-flash|low|chat'
  'antigravity-preview-05-2026|low|interactions'
  'gemini-3.1-flash-lite|low|chat'
  'gemini-3.5-flash-lite|low|chat'
  'gemma-4-31b-it|minimal|chat'
)

mkdir -p "$results_dir"
cd "$backend_dir" || exit 1

for spec in "${models[@]}"; do
  IFS='|' read -r model reasoning protocol <<<"$spec"
  prefix="$results_dir/all-online-20260825-$model"
  raw_done=0
  schema_done=0
  if [[ -s "$prefix.json" ]] && jq -e '.jsonTotal == 11 and .jsonValid == 11 and (.cases | length) == 11' "$prefix.json" >/dev/null 2>&1; then
    raw_done=1
  fi
  if [[ -s "$prefix-schema.json" ]] && jq -e '.jsonTotal == 5 and .jsonValid == 5 and (.cases | length) == 5' "$prefix-schema.json" >/dev/null 2>&1; then
    schema_done=1
  fi
  if [[ "$raw_done" == 1 && "$schema_done" == 1 ]]; then
    printf '\n===== %s: already complete =====\n' "$model"
    continue
  fi

  endpoint="$chat_endpoint"
  protocol_args=()
  if [[ "$protocol" == interactions ]]; then
    endpoint="$interactions_endpoint"
    protocol_args=(-interactions)
  fi
  common=(
    -endpoint "$endpoint"
    -model "$model"
    -reasoning "$reasoning"
    -secret-store "$secret_store"
    -secret-name "$secret_name"
    -skip-warmup
    "${protocol_args[@]}"
  )

  printf '\n===== %s =====\n' "$model"
  if [[ "$raw_done" != 1 ]]; then
    go run ./cmd/llmbench "${common[@]}" -output "$prefix.json" || true
  fi
  if [[ "$schema_done" != 1 ]]; then
    go run ./cmd/llmbench "${common[@]}" -strict-schema \
      -case 'action_interpreter,director,writer_continuation,state_evaluator,choices' \
      -output "$prefix-schema.json" || true
  fi
done
