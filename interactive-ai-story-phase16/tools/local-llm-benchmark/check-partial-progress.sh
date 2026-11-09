#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "--help" ]]; then
  printf '%s\n' \
    'Usage: bash check-partial-progress.sh [1..3]' \
    'Default: three paired repeats (6 story turns, normally 42 model calls).' \
    'Uses the configured Google provider and may consume paid quota.' \
    'Tests the revised local quest prompt in both modes; database prompts stay unchanged.' \
    'Optional environment: TIMELINE_ID (existing timeline UUID).' \
    'Requires running local PostgreSQL and configured provider credentials.' \
    'Reports/logs are saved separately for every pair. No Canon writes.'
  exit 0
fi

repeats=${1:-3}
if (( $# > 1 )) || [[ ! "$repeats" =~ ^[1-3]$ ]]; then
  printf '%s\n' 'Specify 1, 2 or 3 repeats; use --help for details.' >&2
  exit 2
fi
for dependency in go jq tee mktemp timeout; do
  command -v "$dependency" >/dev/null || { printf 'Missing: %s\n' "$dependency" >&2; exit 2; }
done

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
project_dir=$(cd -- "$script_dir/../.." && pwd)
timeline_id=${TIMELINE_ID:-1da4d24b-a530-420c-bc19-2280ad1a42eb}
if [[ ! "$timeline_id" =~ ^[[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12}$ ]]; then
  printf '%s\n' 'TIMELINE_ID must be a UUID.' >&2
  exit 2
fi

mkdir -p -- "$script_dir/results"
result_dir=$(mktemp -d "$script_dir/results/partial-progress-$(date +%Y%m%d-%H%M%S)-XXXXXX")
printf 'Results: %s\n' "$result_dir"

# Freeze only the fully synthetic case; no live story context is sent to models.
jq '.cases |= map(select(.id == "partial-wagons-01"))' \
  "$project_dir/backend/cmd/hybridbench/fixtures/durable-resources-v1.json" > "$result_dir/fixture.json"
jq -e '.revision >= 2 and (.cases | length == 1) and
  (.cases[0].target.objectives | length == 1) and
  (.cases[0].expectedMutations | length > 0)' "$result_dir/fixture.json" >/dev/null

export GOCACHE="${GOCACHE:-$result_dir/go-cache}"
cp -- "$project_dir/backend/prompts/v1/quest_evaluator.md" "$result_dir/quest_evaluator.md"
cd -- "$project_dir/backend"
printf '%s\n' 'Running local tests (details in tests.log)...'
go test ./cmd/hybridbench > "$result_dir/tests.log" 2>&1 || {
  printf 'Local tests failed; see %s/tests.log. No model calls made.\n' "$result_dir" >&2
  exit 1
}
printf '%s\n' 'Building benchmark (details in build.log)...'
go build -o "$result_dir/hybridbench" ./cmd/hybridbench > "$result_dir/build.log" 2>&1 || {
  printf 'Build failed; see %s/build.log. No model calls made.\n' "$result_dir" >&2
  exit 1
}

printf 'Starting %s pair(s), %s turns; normally %s model calls, repairs may add calls.\n' \
  "$repeats" "$((repeats * 2))" "$((repeats * 14))"
overall=0
for (( index=1; index<=repeats; index++ )); do
  report="$result_dir/pair-$index.json"
  printf '\nPair %s/%s\n' "$index" "$repeats"
  set +e
  timeout --foreground --kill-after=10s 20m "$result_dir/hybridbench" \
    -timeline "$timeline_id" -case-set "$result_dir/fixture.json" \
    -runs 1 -profile baseline-safe -planner-mode flash-lite \
    -quest-prompt-file "$result_dir/quest_evaluator.md" \
    -compare-safe-legacy -output "$report" 2>&1 | tee "$result_dir/pair-$index.log"
  statuses=("${PIPESTATUS[@]}")
  set -e
  if (( statuses[0] != 0 || statuses[1] != 0 )); then overall=1; fi
  if [[ ! -s "$report" ]] || (( statuses[1] != 0 )); then
    printf 'Stopped: report missing or log could not be saved. Check pair-%s.log.\n' "$index" >&2
    overall=1
    break
  fi
  jq '{promptRevision, questPromptOverride, caseSetRevision,
    planner: {valid: .summary.validRuns, calls: .summary.totalCalls},
    legacy: {valid: .baselineSummary.validRuns, calls: .baselineSummary.totalCalls},
    failures: [.runs[], .baselineRuns[] |
      select((.error // "") != "" or any(.validatorResults[]; . == false)) |
      {caseId, error, validatorResults}]}' "$report" | tee "$result_dir/pair-$index-summary.json"
  # Do not repeat provider/pipeline failures and spend more quota blindly.
  if jq -e 'any(.runs[], .baselineRuns[]; (.error // "") != "")' "$report" >/dev/null; then
    printf '%s\n' 'Stopped after a pipeline/provider error; inspect the saved report.' >&2
    overall=1
    break
  fi
done
printf '\nFinished (exit %s). Send the pair-*-summary.json files from:\n%s\n' "$overall" "$result_dir"
exit "$overall"
