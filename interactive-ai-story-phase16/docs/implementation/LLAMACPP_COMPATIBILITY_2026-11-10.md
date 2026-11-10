# llama.cpp compatibility — stage 3.9

## Result

The default local chat contract and sequential admission policy are preserved.
A new HTTP regression matrix found and fixed one address bug: a configured full
`/v1/chat/completions` URL was expanded to
`/v1/chat/completions/v1/chat/completions`. Full URLs now pass through unchanged.
Host-root, `/v1`, `/v1/` and Google's `/openai` base shapes retain their existing
behavior.

The strict local HTTP fixture covers four address forms and three response
modes (12 combinations): ordinary JSON, native SSE, and a server ignoring the
requested stream and returning complete JSON. The matrix passed three
race-detector repetitions. It rejects unintended wire extensions, checks local
model/profile identity and validates the resulting text. Streaming is explicit;
default requests contain no `stream`, Google reasoning/agent headers, or
unrequested `cache_prompt`/schema controls. Existing tests continue to cover
schema/stream rejection fallback, partial-stream failures and local concurrency
of one across clients.

No `cache_prompt` flag was introduced. This point has no measured local caching
benefit to justify another setting; the earlier hosted prefix-cache experiment
also did not establish a speed gain. No local model, profile, routing or sampling
default was changed.

## Runtime boundary

The installed `/usr/bin/llama-server` reports version `0.6.0-dev`, build `11429`,
commit `d812350493`. No llama-server process or localhost:8081 listener was
running. The selected Modelfile references
`/home/sk1a/Models/Vikhr-Nemo-12B-Instruct-R-21-09-24-Q5_K_M.gguf`, which is absent.
No replacement model was downloaded or silently substituted.

These checks establish application-side protocol behavior, not inference with
the selected GGUF or a manual browser run. That target-model acceptance gate
remains open.

## Checks

Go formatting/vet/full tests/full race suite against isolated PostgreSQL and
server build passed. Frontend clean install/typecheck/lint, all 57 tests and
production build passed. The same 14 existing npm audit findings remain.

## Next

Stage 3.10 repeats the frozen full-turn baseline with the same configuration,
prompt/fixture hashes, flags, memory exposure and ordering seed where available.
Failures must remain in the report; historical versus current runs are an
observational comparison, not a randomized causal speed claim.
