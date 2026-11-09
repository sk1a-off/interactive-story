#!/usr/bin/env bash
set -euo pipefail
repeats=${1:-3}
if [[ ! "$repeats" =~ ^[1-3]$ ]] || (( $# > 1 )); then
  printf '%s\n' 'Usage: bash check-remaining-gates.sh [1..3] (default 3: 30 turns, normally 210 model calls)' >&2
  exit 2
fi
for command in go jq timeout mktemp; do command -v "$command" >/dev/null; done
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
project_dir=$(cd -- "$script_dir/../.." && pwd)
mkdir -p "$script_dir/results"
result_dir=$(mktemp -d "$script_dir/results/remaining-gates-$(date +%Y%m%d-%H%M%S)-XXXXXX")
printf 'Results: %s\n' "$result_dir"
cp "$project_dir/backend/cmd/hybridbench/fixtures/remaining-gates-v1.json" "$result_dir/fixture.json"
cp "$project_dir/backend/prompts/v1/quest_evaluator.md" "$result_dir/quest_evaluator.md"
jq -e '(.cases | length == 5) and all(.cases[]; .target != null)' "$result_dir/fixture.json" >/dev/null
export GOCACHE="${GOCACHE:-$result_dir/go-cache}"
cd "$project_dir/backend"
printf '%s\n' 'Local tests and build...'
go test ./cmd/hybridbench > "$result_dir/tests.log" 2>&1
go build -o "$result_dir/hybridbench" ./cmd/hybridbench > "$result_dir/build.log" 2>&1
mapfile -t cases < <(jq -r '.cases[].id' "$result_dir/fixture.json")
overall=0
for (( repeat=1; repeat<=repeats; repeat++ )); do
  for case_id in "${cases[@]}"; do
    stem="$result_dir/$case_id-$repeat"
    printf 'Start %s repeat %s/%s\n' "$case_id" "$repeat" "$repeats"
    if timeout --foreground --kill-after=10s 20m "$result_dir/hybridbench" \
      -timeline "${TIMELINE_ID:-1da4d24b-a530-420c-bc19-2280ad1a42eb}" \
      -case-set "$result_dir/fixture.json" -case-filter "$case_id" -runs 1 \
      -profile baseline-safe -planner-mode flash-lite -compare-safe-legacy \
      -quest-prompt-file "$result_dir/quest_evaluator.md" -output "$stem.json" > "$stem.log" 2>&1; then
      status=0
    else
      status=$?
      overall=1
    fi
    if [[ ! -s "$stem.json" ]]; then
      printf 'Stopped: no report, exit %s; inspect %s.log\n' "$status" "$stem" >&2
      exit 1
    fi
    jq '{caseSetRevision,promptRevision,questPromptOverride,
      planner:.summary.validRuns,legacy:.baselineSummary.validRuns,
      failures:[.runs[],.baselineRuns[]|select((.error // "") != "" or any(.validatorResults[];.==false))|{caseId,error,validatorResults}]}' \
      "$stem.json" > "$stem-summary.json"
    jq -r '"Finished: Planner \(.planner)/1 Legacy \(.legacy)/1"' "$stem-summary.json"
    if jq -e 'any(.runs[],.baselineRuns[];(.error // "") != "")' "$stem.json" >/dev/null; then
      printf '%s\n' 'Stopped after provider/pipeline error to avoid spending further quota.' >&2
      exit 1
    fi
  done
done
printf 'Finished all pairs, exit %s. Reports: %s\n' "$overall" "$result_dir"
exit "$overall"
