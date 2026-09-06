# Generation Workflow Experiment Result

Дата: 2026-09-05

## Решение

**MODIFY. Полный V2 workflow пока нельзя переводить в реализацию или production rollout по исходному плану.**

Подтверждены две части гипотезы:

1. Safe Hybrid (`gemini-3.5-flash-lite` только для `action_interpreter`, `pacing`, `choices`) на текущем snapshot существенно быстрее Antigravity-only и не дал обнаруженных критических Canon-ошибок в свежей парной серии.
2. Текущий pipeline действительно многократно передаёт один и тот же большой `generationtarget.Target`; role-specific context имеет большой потенциал сокращения payload.

Одновременно получены два блокирующих результата:

1. Агрессивная передача evaluators на Flash-Lite снова создаёт семантически ложные или преждевременные Canon mutations, хотя JSON и Go validators проходят.
2. Memory gate не пройден: `ContextBuilder` отсутствует в live generation path, живых memory rows нет, dedupe нет, а требуемый graceful fallback при retrieval error не реализован.

Planner, Canon Extractor, summaries, provisional Beat и большая часть Tests 7–20 ещё не существуют даже как benchmark-only prototypes. Поэтому их нельзя честно отметить как PASS.

## Environment

- Workspace: `interactive-ai-story-phase16`
- Timeline: `1da4d24b-a530-420c-bc19-2280ad1a42eb`, «Наследник Пожирателей»
- Snapshot head: 264 до и после benchmark
- Prompt Set revision: 5
- Active AI config revision: 24
- Fast model: `gemini-3.5-flash-lite`
- Quality model: `antigravity-preview-05-2026`
- Provider: Google AI OpenAI-compatible endpoint
- PostgreSQL: local pgvector/PostgreSQL 16
- Canon writes: disabled in live benchmark through in-memory `captureCanon`

Ограничение выборки: свежий live benchmark использует одну насыщенную timeline group и четыре текущих Reader actions, а не требуемые 8–12 fixture groups. Поэтому статистические p95 и литературная human evaluation пока предварительны.

## Baseline

Свежий строго парный тест использовал те же три actions и один замороженный snapshot.

| Profile | Runs | Valid | Median total | Max | Average words |
|---|---:|---:|---:|---:|---:|
| Safe Hybrid | 3 | 3/3 | 126.01 s | 128.85 s | 405.3 |
| Antigravity-only | 3 | 3/3 | 232.73 s | 254.06 s | 439.0 |

Safe Hybrid уменьшил median total latency на **45.9%** (1.85x быстрее). Все шесть результатов имели непустой Beat, четыре абзаца и четыре distinct choices.

Главные median role latencies:

| Role | Safe Hybrid | Antigravity-only |
|---|---:|---:|
| action_interpreter | 1.18 s | 19.97 s |
| pacing | 1.12 s | 76.33 s |
| choices | 1.72 s | 21.35 s |
| director | 20.96 s | 21.00 s |
| writer | 36.80 s | 36.10 s |

Вывод: текущий Safe Hybrid routing подтверждён как полезный baseline. Эта серия не проверяет новый Turn Planner и не доказывает H1 сама по себе, потому что число стадий осталось равным восьми.

## Aggressive Flash-Lite evaluator test

Проверенный профиль: Flash-Lite для `action_interpreter`, `pacing`, `world_evaluator`, `state_evaluator`, `quest_evaluator`, `choices`; Antigravity для `director`, `writer`.

| Runs | JSON/validator valid | Median total | Range | Average words |
|---:|---:|---:|---:|---:|
| 4 | 4/4 | 61.00 s | 52.45–74.04 s | 423.8 |

Это ещё на 51.6% быстрее свежего Safe Hybrid, но профиль получил **NO-GO** из-за Canon-quality failures:

- В action про проверку крепления клети `state_evaluator` создал `journal_entry_updated` для походного пайка с evidence «Спрятав полученный паек…», отсутствующим в новом Beat и взятым из старого context.
- В том же turn objective «Штормовые растяжки обоза» был помечен completed/100, хотя текст заканчивается до закрепления клети и предлагает игроку выбрать способ её фиксации.
- В другом turn stage был завершён после закрепления только головного фургона, при том что success criteria сформулирован для повозок каравана в целом. Это как минимум неоднозначная преждевременная completion и требует human adjudication.

Текущий `validateJournalChanges` требует лишь непустой evidence, но не проверяет, что evidence поддерживается `newBeat`. Поэтому semantic false positive проходит application validators.

Результат согласуется с тестами 2026-08-27: тогда агрессивный профиль имел один invalid output и одну ложную state mutation в девяти полных runs. Новый прогон показывает, что проблема не устранена.

## Context duplication audit

Для одного live turn добавлена безопасная instrumentation, сохраняющая только размеры inputs, но не сами role inputs.

- Full `context` JSON: 104,632 bytes.
- Sum of eight role input payloads: 1,003,629 bytes.
- Full context repeated across roles: 837,056 bytes, **83.4%** суммарного payload.
- Exact top-level duplicates already present inside `context`: 72,001 bytes, **7.17%** payload.

Наиболее крупные role payloads:

| Role | Input bytes | Full context | Exact duplicated top-level fields |
|---|---:|---:|---:|
| choices | 148,495 | 104,632 | 22,396 |
| quest_evaluator | 137,777 | 104,632 | 13,661 |
| writer | 127,697 | 104,632 | 8,738 |
| director | 127,335 | 104,632 | 8,738 |
| world_evaluator | 119,819 | 104,632 | 9,730 |
| state_evaluator | 118,788 | 104,632 | 8,734 |

Конкретные безопасные кандидаты для первого dedupe experiment:

- не передавать `heroJournal` одновременно отдельно и внутри `context`;
- не передавать `objectives`/`currentObjectives` одновременно отдельно и внутри `context`;
- не передавать `currentCharacters` и `currentWorld` отдельно, если роль уже получает тот же полный context;
- после сохранения информационного покрытия заменить full Target на явные role-specific DTO.

Это подтверждает потенциал H5 по размеру, но continuity gate ещё не проверен.

## Memory + Context gate

Статус: **FAIL / BLOCKED**.

Что прошло в существующих unit tests:

- application-layer фильтрация StoryID/TimelineID/owner/head;
- исключение sibling и expired rows, возвращённых fake retriever;
- authoritative state не удаляется soft token budget.

Что блокирует Test 6:

- `ContextBuilder` нигде не создаётся и не вызывается live generation orchestration;
- current database содержит 0 rows в `memory_embeddings`;
- нет integration test, доказывающего доставку long-history fact в Writer shadow context;
- нет dedupe по memory ID или совпадению с recent Beat;
- retrieval error возвращает пустой `Result` с ошибкой; требуемый fallback на authoritative state + recent context не реализован;
- отсутствуют long-history fixtures и recall@K/leakage measurements.

Следствие: compact context, semantic retrieval и summaries нельзя включать в рекомендуемый production profile.

## Status of hypotheses

| Hypothesis | Status | Result |
|---|---|---|
| H1 fewer sequential calls reduce latency | PARTIAL | Routing proves expensive structural calls matter, but Planner call-count reduction is untested. |
| H2 Flash-Lite Turn Planner quality | NOT TESTED | No prompt, contract, fixture set, validator or prototype. |
| H3 Flash-Lite Canon Extractor quality | NOT TESTED | No extractor prototype/golden dataset. Existing separate Flash-Lite evaluators are unsafe. |
| H4 one Extractor faster without lost changes | NOT TESTED | No E0–E3 paired experiment. |
| H5 role-specific context reduces tokens safely | PARTIAL | Duplication is confirmed; continuity ablation is blocked by memory gate. |
| H6 retrieval compensates compact history | FAIL/BLOCKED | No live wiring, data or probes. |
| H7 provisional Writer improves UX safely | NOT TESTED | Protocol/UI state does not exist. |
| H8 V2 reduces Antigravity calls | NOT TESTED | V2 orchestration does not exist. |
| H9 ContextBuilder works live | FAIL | No call site in generation path. |
| H10 summaries preserve memory | NOT TESTED | No summary lifecycle/prototype. |
| H11 every change has a regression gate | FAIL | Most proposed changes and gates do not exist yet. |

## Status of Tests 0–20

| Test | Status | Evidence |
|---:|---|---|
| 0 baseline | PARTIAL PASS | Fresh paired 3+3 live runs; dataset breadth below plan. |
| 1 raw role capability | MODIFY | Safe fast roles pass preliminarily; evaluator routing fails semantic safety. |
| 2 Turn Planner | NOT IMPLEMENTED | No benchmark-only contract/path. |
| 3 Writer sensitivity | NOT IMPLEMENTED | No planner variants or blind bundle. |
| 4 Canon Extractor | NOT IMPLEMENTED | No extractor/golden labels. |
| 5 context duplication | PASS | Size-only live instrumentation and report. |
| 6 ContextBuilder live shadow | FAIL | Partial unit coverage only; no live wiring/data/dedupe/fallback. |
| 7 context ablation | BLOCKED | Test 6 is not passed. |
| 8 summaries | NOT IMPLEMENTED | No generation/lifecycle path. |
| 9 Conditional Lore | NOT APPLICABLE TO CURRENT TREE | Current dirty tree explicitly removes world-rule/lore behavior; product decision is needed before restoring this scope. |
| 10 Choices isolation | FAIL FOR TARGET POLICY | Current pipeline retries choices but fails the whole turn if choices remain invalid; no deterministic fallback commit. |
| 11 stage-aware retry | NOT IMPLEMENTED | Durable worker retries the whole pipeline; no stage checkpoints. |
| 12 provisional backend | NOT IMPLEMENTED | No `draft_ready`, `draft_replaced`, `committed` phases. |
| 13 provisional frontend | NOT IMPLEMENTED | No provisional Reader state machine. |
| 14 timeout budget | PARTIAL | Fresh role latency ranges captured; sample too small for release p95. |
| 15 concurrency/quota | BLOCKED | Proposed parallel V2 stages/semaphores do not exist. |
| 16 same-head competing actions | FAIL | Active job lookup deduplicates only by timeline/head and can return A's generation ID to a different action B. |
| 17 observability/provenance/flags | PARTIAL/FAIL | Config and Prompt Set revision are pinned; stage metrics, role contract/context mode provenance and V2 flags absent. |
| 18 Reader SSE/polling | FAIL FOR TARGET POLICY | SSE replay exists, but Reader still polls `/current` every second while idle. |
| 19 Canon/Save/Fork/replay | BASELINE PASS, V2 NOT TESTABLE | PostgreSQL atomic commit, stale-head and Save/Fork tests pass; no V2 exists. |
| 20 final V2 E2E/rollback | NOT IMPLEMENTED | No profiles D–F; no 30–100 turn sequence. |

## Regression evidence

Passed on the current dirty working tree:

- `go test ./...`
- PostgreSQL integration suite with `DATABASE_URL`: 7/7 tests
- frontend Vitest: 6 files, 19 tests
- frontend typecheck
- frontend ESLint with zero warnings
- current timeline event sequence: 264 events, min 1, max 264, zero gaps, head remains 264

These are baseline regression results only, not V2 acceptance evidence.

## Recommended production profile now

Keep current Safe Hybrid routing:

```text
gemini-3.5-flash-lite: action_interpreter, pacing, choices
antigravity: director, writer, world_evaluator, state_evaluator, quest_evaluator
```

Do not enable evaluator Flash-Lite routing, compact context, retrieval, summaries or provisional UX as defaults.

## Required next experiment phase before rewriting implementation details

The implementation plan should be rewritten around verified gates rather than assuming the full V2 is already validated:

1. Build a benchmark-only 8–12 category fixture set and golden durable deltas.
2. Add evidence-to-`newBeat` semantic entailment protection before any fast evaluator/extractor rollout.
3. Prototype Turn Planner only in `hybridbench`; compare legacy vs Flash-Lite vs Antigravity on identical inputs.
4. Prototype Canon Extractor only after golden labels exist.
5. Wire ContextBuilder in shadow mode with authoritative fallback, dedupe and real long-history rows before any compact-context ablation.
6. Treat provisional Beat, stage-aware retries, same-head conflict semantics and polling cleanup as implementation phases followed by acceptance tests, not as pre-implementation hypotheses that can be marked PASS today.
7. Resolve whether world rules/Lore Guard remain removed; the current working tree and the supplied plans currently contradict each other.

Overall decision remains **MODIFY**, with Safe Hybrid retained as the measured baseline.

## Artifacts

- `tools/local-llm-benchmark/results/generation-workflow-baseline-safe-hybrid-20260905.json`
- `tools/local-llm-benchmark/results/generation-workflow-aggressive-hybrid-20260905.json`
- `tools/local-llm-benchmark/results/generation-workflow-context-audit-20260905.json`
- `backend/cmd/hybridbench/main.go` size-only input instrumentation
- `backend/cmd/hybridbench/main_test.go` instrumentation tests

## Implementation continuation (same date)

The gated implementation continued without changing the production model
routing. The local database is now at migration 35.

- Phase 0/1: the benchmark has a revisioned 12-category fixture set, repeat and
  case filters, per-stage timing, role-call provenance, repair/escalation counts,
  and golden durable-delta validation. A full 12 x 3 paired Gate 0 run is still
  outstanding.
- Phase 2/3: new-Beat evidence validation, conservative objective-completion
  support, no-op filtering, normalized action hashes, same-head conflict
  semantics, and lossless role-context dedupe are implemented.
- Phase 4: ContextBuilder is wired behind `GENERATION_CONTEXT_SHADOW_V1`, with
  authoritative fallback, scope filtering, deterministic ordering, and dedupe.
  The live gate remains blocked because the configured embedding service is not
  available and `memory_embeddings` has no rows.
- Phase 5: benchmark-only Flash-Lite Planner reduced the observed planner stage
  from 18.112 s to 1.885 s on one case. This sample is promising but far below
  the Gate 5 breadth requirement, so production remains on the legacy planner.
- Phase 7: the Flash-Lite Extractor failed both first-pass trials. A bounded E2
  repair corrected `evidence` to `evidenceQuote`, but retained a non-verbatim
  ellipsis-composed quote. The evidence gate rejected it and Canon remained
  untouched. Gate 7 remains failed; production remains on separate Antigravity
  evaluators.
- Phase 8: invalid Choices receive one bounded retry and then four deterministic
  distinct fallback actions without rerunning Writer.
- Phase 6/12 scaffold: durable provisional updates and Reader preview are behind
  `GENERATION_PROVISIONAL_BEAT_V1` (off by default). Updates carry timeline,
  sequence, revision, and provisional status; Reader keys them by timeline and
  generation. Idle snapshot polling is reduced from one second to a 60-second
  safety interval, with SSE driving active progress.

Additional live artifacts:

- `tools/local-llm-benchmark/results/turn-planner-p0-legacy-conversation-r18.json`
- `tools/local-llm-benchmark/results/turn-planner-p1-flash-lite-conversation-r18.json`
- `tools/local-llm-benchmark/results/turn-planner-p2-antigravity-conversation-r18.json`
- `tools/local-llm-benchmark/results/canon-extractor-e1-flash-lite-conversation-r18.json`
- `tools/local-llm-benchmark/results/canon-extractor-e1-flash-lite-conversation-r18-v2.json`
- `tools/local-llm-benchmark/results/canon-extractor-e2-flash-lite-repair-conversation-r18.json`
