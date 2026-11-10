package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
)

const legacyEmptyPromptHash = "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"

type turnComparison struct {
	BeforeSHA256, AfterSHA256 string
	Before, After             []turnArmSummary
	Deltas                    []turnComparisonDelta
	BeforeHTTP, AfterHTTP     turnHTTPCounts
	Notes                     []string
}
type turnHTTPCounts struct {
	Posts, Gets, TransportFailures int
	Statuses                       map[int]int
}
type turnComparisonDelta struct {
	Memory                  bool
	SuccessfulMatches       int
	FirstText, Ready, Total turnTimings // after minus before for matching successes
}
type comparisonKey struct {
	Case   string
	Repeat int
	Memory bool
}

func comparisonRows(r turnBenchmarkReport) (map[comparisonKey]turnBenchmarkRun, error) {
	if r.Mode != "paired-full-turn-v1" || r.FinishedAt.IsZero() || !r.CleanupVerified ||
		(r.Status != "measured" && r.Status != "smoke_only" && !(r.Status == "failed" && r.Failure == "incomplete_or_failed_turns")) {
		return nil, errors.New("comparison requires finished reports with verified cleanup")
	}
	rows := map[comparisonKey]turnBenchmarkRun{}
	for _, row := range r.Runs {
		key := comparisonKey{row.Case, row.Repeat, row.Memory}
		if _, exists := rows[key]; exists || key.Case == "" || key.Repeat < 1 || key.Repeat > r.Repeats {
			return nil, errors.New("comparison contains duplicate or invalid rows")
		}
		if row.ElapsedMS <= 0 || !row.Memory && row.MemoryDocuments != 0 || row.FirstAvailableTextMS != nil && (*row.FirstAvailableTextMS < 0 || *row.FirstAvailableTextMS > row.ElapsedMS) || row.ReadyMS != nil && (*row.ReadyMS < 0 || *row.ReadyMS > row.ElapsedMS) {
			return nil, errors.New("comparison contains invalid timing or memory bounds")
		}
		if row.Succeeded && (row.ReadyMS == nil || row.FirstAvailableTextMS == nil || row.Choices != 4 || row.Memory && row.MemoryDocuments == 0) {
			return nil, errors.New("successful row lacks complete timing, choices or memory exposure")
		}
		rows[key] = row
	}
	if len(rows) == 0 {
		return nil, errors.New("comparison contains no turns")
	}
	for key := range rows {
		for repeat := 1; repeat <= r.Repeats; repeat++ {
			for _, memory := range []bool{false, true} {
				if _, ok := rows[comparisonKey{key.Case, repeat, memory}]; !ok {
					return nil, errors.New("comparison contains an incomplete scenario")
				}
			}
		}
	}
	return rows, nil
}

func countTurnHTTP(rows []turnBenchmarkRun) turnHTTPCounts {
	r := turnHTTPCounts{Statuses: map[int]int{}}
	for _, row := range rows {
		for _, call := range row.Calls {
			r.Posts += call.HTTPPosts
			r.Gets += call.HTTPGets
			r.TransportFailures += call.TransportFailures
			for _, status := range call.HTTPStatuses {
				r.Statuses[status]++
			}
		}
	}
	return r
}

func compareTurns(before, after turnBenchmarkReport) (turnComparison, error) {
	var result turnComparison
	if before.FixtureSHA256 == "" || after.FixtureSHA256 == "" || before.Provider == "configured" && (before.ConfigID.IsZero() || before.PromptID.IsZero() || before.ConfigRevision < 1 || before.PromptRevision < 1) {
		return result, errors.New("comparison requires frozen fixture and configured revision identities")
	}
	// Stored revision identity is separate from content hashes. Profiles absent
	// in older reports resolve to quality, exactly as old settings do at runtime.
	identity := func(r turnBenchmarkReport) any {
		return []any{r.Mode, r.Provider, r.ConfigID, r.ConfigRevision, r.PromptID, r.PromptRevision,
			r.FixtureSHA256, r.PrimaryModel, r.SafeHybrid, r.Planner, r.Provisional,
			r.FastState, r.JournalIsolation, r.JSONSchema, r.GenerationProfile.Effective(), r.Embedding, r.Repeats, r.Seed}
	}
	if !reflect.DeepEqual(identity(before), identity(after)) || !before.GenerationProfile.Available() || !after.GenerationProfile.Available() {
		return result, errors.New("comparison configuration, prompts, fixtures, ordering or chain differ")
	}
	if after.PromptSHA256 == "" || after.PromptSHA256 == legacyEmptyPromptHash {
		return result, errors.New("after report requires an actual prompt-content fingerprint")
	}
	if before.PromptSHA256 == legacyEmptyPromptHash && !before.PromptID.IsZero() && before.PromptRevision > 0 {
		result.Notes = append(result.Notes, "Legacy before fingerprint hashed {} rather than prompts. Prompt ID/revision match; historical content hash cannot independently prove equality.")
	} else if before.PromptSHA256 != after.PromptSHA256 {
		return result, errors.New("comparison prompt-content fingerprints differ")
	}
	oldRows, err := comparisonRows(before)
	if err != nil {
		return result, err
	}
	newRows, err := comparisonRows(after)
	if err != nil || len(oldRows) != len(newRows) {
		return result, errors.New("comparison reports contain incomplete or different turn sets")
	}
	for key, old := range oldRows {
		row, ok := newRows[key]
		if !ok || old.Order != row.Order || old.Succeeded && row.Succeeded && old.MemoryDocuments != row.MemoryDocuments {
			return result, errors.New("comparison scenario order or effective memory exposure differs")
		}
	}
	result.Before, result.After = summarizeTurnRuns(before.Runs), summarizeTurnRuns(after.Runs)
	result.BeforeHTTP, result.AfterHTTP = countTurnHTTP(before.Runs), countTurnHTTP(after.Runs)
	for _, memory := range []bool{false, true} {
		var first, ready, total []float64
		for key, old := range oldRows {
			row := newRows[key]
			if key.Memory != memory || !old.Succeeded || !row.Succeeded {
				continue
			}
			first = append(first, *row.FirstAvailableTextMS-*old.FirstAvailableTextMS)
			ready = append(ready, *row.ReadyMS-*old.ReadyMS)
			total = append(total, row.ElapsedMS-old.ElapsedMS)
		}
		result.Deltas = append(result.Deltas, turnComparisonDelta{Memory: memory, SuccessfulMatches: len(total), FirstText: turnDistribution(first), Ready: turnDistribution(ready), Total: turnDistribution(total)})
	}
	result.Notes = append(result.Notes,
		"Historical versus current observations are not a randomized causal comparison. Provider load, quotas, generated text and token counts can differ.",
		"Timing summaries include successful turns; failures and their physical calls remain in counts. Matched deltas exclude any pair with a failed turn.",
		"First text is pipeline preview availability, not browser rendering. Queue wait and real SQL Canon commit remain excluded.")
	return result, nil
}

func writeTurnComparison(beforePath, afterPath, output string) error {
	if output == "" {
		return errors.New("separate comparison output required")
	}
	outAbs, err := filepath.Abs(output)
	if err != nil {
		return errors.New("invalid comparison output")
	}
	for _, source := range []string{beforePath, afterPath} {
		abs, err := filepath.Abs(source)
		if err != nil || abs == outAbs {
			return errors.New("separate comparison output required")
		}
		inputStat, inputErr := os.Stat(source)
		outputStat, outputErr := os.Stat(output)
		if inputErr == nil && outputErr == nil && os.SameFile(inputStat, outputStat) {
			return errors.New("comparison output aliases an input")
		}
	}
	read := func(path string) (turnBenchmarkReport, []byte, error) {
		raw, err := os.ReadFile(path)
		var report turnBenchmarkReport
		if err == nil {
			err = json.Unmarshal(raw, &report)
		}
		return report, raw, err
	}
	before, rawBefore, err := read(beforePath)
	if err != nil {
		return errors.New("before report unavailable")
	}
	after, rawAfter, err := read(afterPath)
	if err != nil {
		return errors.New("after report unavailable")
	}
	result, err := compareTurns(before, after)
	if err != nil {
		return err
	}
	hash := func(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
	result.BeforeSHA256, result.AfterSHA256 = hash(rawBefore), hash(rawAfter)
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(output, append(raw, '\n'), 0600)
}
