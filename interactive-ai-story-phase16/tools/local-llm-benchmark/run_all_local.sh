#!/usr/bin/env bash
set -uo pipefail

workspace=/home/sk1a/Projects/interactive-ai-story-phase16-result/interactive-ai-story-phase16
models_dir=/home/sk1a/Models
results_dir="$workspace/tools/local-llm-benchmark/results"
backend_dir="$workspace/backend"
port=18081
server_pid=""

cleanup_server() {
  if [[ -n "$server_pid" ]]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
    server_pid=""
  fi
}
trap cleanup_server EXIT INT TERM

models=(
  'qwen35-9b-uncensored-aggressive-q4km|Qwen3.5-9B-Uncensored-HauhauCS-Aggressive-Q4_K_M.gguf|1'
  'gemma4-e4b-uncensored-aggressive-q8kp|Gemma-4-E4B-Uncensored-HauhauCS-Aggressive-Q8_K_P.gguf|0'
  'vikhr-nemo-12b-q5km|Vikhr-Nemo-12B-Instruct-R-21-09-24-Q5_K_M.gguf|0'
  'qwen3-14b-abliterated-q4km|qwen3-14b-abliterated-q4_k_m.gguf|1'
  'qwen35-9b-uncensored-aggressive-q8|Qwen3.5-9B-Uncensored-HauhauCS-Aggressive-Q8_0.gguf|1'
  'runeweaver-rp-ru-12b-q6k|MN-12B-Runeweaver-RP-RU.Q6_K.gguf|0'
  'dark-champion-8x3b-q4km|L3.2-8X3B-MOE-Dark-Champion-Inst-18.4B-uncen-ablit_D_AU-Q4_k_m.gguf|0'
  'qwen36-27b-uncensored-aggressive-q2kp|Qwen3.6-27B-Uncensored-HauhauCS-Aggressive-Q2_K_P.gguf|1'
  'qwen36-27b-uncensored-aggressive-iq3m|Qwen3.6-27B-Uncensored-HauhauCS-Aggressive-IQ3_M.gguf|1'
  'qwen38-27b-uncensored-aggressive-q3kp|Qwen3.8-27B-Uncensored-HauhauCS-Aggressive-Q3_K_P.gguf|1'
  'cydonia-24b-v4.3-q4km|TheDrummer_Cydonia-24B-v4.3-Q4_K_M.gguf|0'
)

mkdir -p "$results_dir"
cd "$backend_dir" || exit 1

for spec in "${models[@]}"; do
  IFS='|' read -r slug filename no_think <<<"$spec"
  model_path="$models_dir/$filename"
  prefix="$results_dir/all-20260824-$slug"
  server_log="$prefix-server.log"
  runtime_log="$prefix-runtime.txt"

  raw_done=0
  schema_done=0
  if [[ -s "$prefix.json" ]] && jq -e '.jsonTotal == 11 and (.cases | length) == 11' "$prefix.json" >/dev/null 2>&1; then
    raw_done=1
  fi
  if [[ -s "$prefix-schema.json" ]] && jq -e '.jsonTotal == 11 and (.cases | length) == 11' "$prefix-schema.json" >/dev/null 2>&1; then
    schema_done=1
  fi
  if [[ "$raw_done" == 1 && "$schema_done" == 1 ]]; then
    printf '\n===== %s: already complete =====\n' "$filename"
    continue
  fi

  printf '\n===== %s =====\n' "$filename"
  /usr/bin/llama-server -m "$model_path" --host 127.0.0.1 --port "$port" \
    -c 16384 -np 1 -ngl auto -fit on -fitt 512 -fa on -ctk q8_0 -ctv q8_0 \
    --jinja --no-webui >"$server_log" 2>&1 &
  server_pid=$!

  ready=0
  for _ in $(seq 1 240); do
    if curl -fsS "http://127.0.0.1:$port/health" >/dev/null 2>&1; then
      ready=1
      break
    fi
    if ! kill -0 "$server_pid" 2>/dev/null; then
      break
    fi
    sleep 1
  done
  if [[ "$ready" != 1 ]]; then
    printf 'server failed: %s\n' "$filename"
    tail -80 "$server_log"
    cleanup_server
    continue
  fi

  {
    printf 'model=%s\n' "$filename"
    nvidia-smi --query-compute-apps=pid,process_name,used_memory --format=csv,noheader
  } >"$runtime_log"

  extra=()
  if [[ "$no_think" == 1 ]]; then
    extra=(-no-think-token)
  fi
  if [[ "$raw_done" != 1 ]]; then
    go run ./cmd/llmbench \
      -endpoint "http://127.0.0.1:$port/v1/chat/completions" \
      -model "$slug" "${extra[@]}" \
      -output "$prefix.json" || true
  fi
  if [[ "$schema_done" != 1 ]]; then
    go run ./cmd/llmbench \
      -endpoint "http://127.0.0.1:$port/v1/chat/completions" \
      -model "$slug" "${extra[@]}" -strict-schema \
      -output "$prefix-schema.json" || true
  fi

  cleanup_server
done
