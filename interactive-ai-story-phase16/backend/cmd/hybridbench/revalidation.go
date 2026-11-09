package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type reportRevalidation struct {
	SourceReport          string    `json:"sourceReport"`
	SourceCaseSetRevision int       `json:"sourceCaseSetRevision"`
	CheckedAt             time.Time `json:"checkedAt"`
}

// This rechecks recorded outputs; it does not regenerate against changed inputs.
// Exclusive output creation preserves the original report, including aliases.
func revalidateSavedReport(source, casePath, output string) (benchmarkReport, error) {
	var report benchmarkReport
	if source == "" || casePath == "" || output == "" {
		return report, fmt.Errorf("revalidation requires source, case-set and a new output path")
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return report, err
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return report, err
	}
	set, err := loadCaseSet(casePath)
	if err != nil {
		return report, err
	}
	if report.CaseSet != set.Name || len(report.Runs) == 0 {
		return report, fmt.Errorf("case-set mismatch or empty source report")
	}
	byID := map[string]benchmarkCase{}
	for _, item := range set.Cases {
		byID[item.ID] = item
	}
	for _, group := range [][]runResult{report.Runs, report.BaselineRuns} {
		for index := range group {
			run := &group[index]
			item, found := byID[run.CaseID]
			if !found || item.Target == nil || item.Action != run.Action {
				return report, fmt.Errorf("case %q missing, changed action or lacks synthetic target", run.CaseID)
			}
			run.ValidatorResults = validateCase(run.Events, item, *item.Target)
			run.ExpectedImportantFacts = append([]string(nil), item.ExpectedImportantFacts...)
		}
	}
	report.Revalidation = &reportRevalidation{SourceReport: source, SourceCaseSetRevision: report.CaseSetRevision, CheckedAt: time.Now().UTC()}
	report.CaseSetRevision = set.Revision
	report.Summary = summarize(report.Runs)
	if len(report.BaselineRuns) > 0 {
		report.BaselineSummary = summarize(report.BaselineRuns)
		report.PairedDelta = pairedDelta(report.Runs, report.BaselineRuns)
	}
	raw, err = json.MarshalIndent(report, "", "  ")
	if err != nil {
		return report, err
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return report, err
	}
	_, writeErr := file.Write(append(raw, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		return report, writeErr
	}
	return report, closeErr
}
