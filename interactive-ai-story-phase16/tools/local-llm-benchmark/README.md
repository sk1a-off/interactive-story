# Local LLM benchmark

This benchmark sends the project's current prompt contracts to an OpenAI-compatible endpoint and records strict JSON validity, instruction/language/continuity checks, quest-operation consistency, hero journal/inventory/currency updates, latency, and generated outputs. Prose cases also record word/paragraph counts, duplicate paragraphs, and the repeated six-gram ratio so loops inside a single completion are visible.

From `backend`:

```bash
go run ./cmd/llmbench \
  -endpoint http://127.0.0.1:11434/v1/chat/completions \
  -model bench-qwen35-9b-q8 \
  -output ../tools/local-llm-benchmark/results/qwen35-9b-q8.json
```

Imported local GGUF candidates use the checked-in Modelfiles under `models/`. The generated result JSON intentionally includes full outputs so the qualitative verdict can be audited rather than inferred from aggregate scores alone.

Run every complete GGUF found in the project benchmark inventory, first with plain JSON and then with strict per-role schemas:

```bash
./tools/local-llm-benchmark/run_all_local.sh
```

Run all Google models exposed by the project settings. Credentials are read from the project's secret store and are never passed as command-line values or written to result JSON:

```bash
./tools/local-llm-benchmark/run_all_online.sh
```

The online runner uses 11 plain-JSON cases plus five critical strict-schema cases so one complete run fits Google's 20-request daily free-tier allowance. A model whose quota was already used earlier that day may still finish only partially; incomplete result files are retained for diagnosis and retried by the next run.

For a direct llama.cpp server, use one slot so the full configured context is available to the request:

```bash
llama-server \
  -m /absolute/path/to/model.gguf \
  --host 127.0.0.1 --port 8081 \
  -c 16384 -np 1 -ngl auto -fit on -fitt 512 \
  -fa on -ctk q8_0 -ctv q8_0 --jinja --no-webui
```
