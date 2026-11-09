package main

import (
	"encoding/json"
	"fmt"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
	"os"
	"strings"
)

type goldenDelta struct {
	EventType string `json:"eventType"`
	Minimum   int    `json:"minimum"`
	Maximum   int    `json:"maximum"`
}

type benchmarkCase struct {
	Target                 *generationtarget.Target `json:"target,omitempty"`
	ExpectedMutations      []expectedMutation       `json:"expectedMutations,omitempty"`
	Validators             []semanticValidator      `json:"validators,omitempty"`
	ID                     string                   `json:"id"`
	Category               string                   `json:"category"`
	Action                 string                   `json:"action"`
	ExpectedImportantFacts []string                 `json:"expectedImportantFacts"`
	GoldenDurableDeltas    []goldenDelta            `json:"goldenDurableDeltas"`
}

type benchmarkCaseSet struct {
	Name     string          `json:"name"`
	Revision int             `json:"revision"`
	Cases    []benchmarkCase `json:"cases"`
}

func loadCaseSet(path string) (benchmarkCaseSet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return benchmarkCaseSet{}, err
	}
	var set benchmarkCaseSet
	if err = json.Unmarshal(raw, &set); err != nil {
		return benchmarkCaseSet{}, err
	}
	if set.Name == "" || set.Revision < 1 || len(set.Cases) < 1 {
		return benchmarkCaseSet{}, fmt.Errorf("invalid benchmark case set metadata")
	}
	seen := map[string]bool{}
	for index, item := range set.Cases {
		if item.ID == "" || item.Category == "" || item.Action == "" || seen[item.ID] {
			return benchmarkCaseSet{}, fmt.Errorf("invalid or duplicate case at index %d", index)
		}
		seen[item.ID] = true
		for _, expectation := range item.ExpectedMutations {
			if err := expectation.validate(); err != nil {
				return benchmarkCaseSet{}, fmt.Errorf("case %s: %w", item.ID, err)
			}
		}
		if item.Target != nil && (item.Target.SceneID.IsZero() || item.Target.ChapterID.IsZero() || item.Target.NextBeatPosition < 1) {
			return benchmarkCaseSet{}, fmt.Errorf("case %s: invalid fixture target", item.ID)
		}
		for _, validator := range item.Validators {
			if validator.Kind != "scene_transition" && validator.Kind != "journal_state" && validator.Kind != "objective_state" && validator.Kind != "objectives_remain_active" && validator.Kind != "objective_progress_unchanged" {
				return benchmarkCaseSet{}, fmt.Errorf("case %s: unknown validator %q", item.ID, validator.Kind)
			}
		}
	}
	return set, nil
}

func fastRolesForProfile(profile string) (map[string]bool, error) {
	profiles := map[string][]string{
		"baseline-safe":     {"action_interpreter", "pacing", "choices"},
		"aggressive-hybrid": {"action_interpreter", "pacing", "world_evaluator", "state_evaluator", "quest_evaluator", "choices"},
		"antigravity-only":  {},
	}
	roles, ok := profiles[profile]
	if !ok {
		return nil, fmt.Errorf("unknown profile %q", profile)
	}
	selected := make(map[string]bool, len(roles))
	for _, role := range roles {
		selected[role] = true
	}
	return selected, nil
}

func expandCaseRuns(cases []benchmarkCase, repeats int) []benchmarkCase {
	if repeats < 1 {
		return nil
	}
	out := make([]benchmarkCase, 0, len(cases)*repeats)
	for _, item := range cases {
		for repeat := 0; repeat < repeats; repeat++ {
			out = append(out, item)
		}
	}
	return out
}

func filterCases(cases []benchmarkCase, selector string) ([]benchmarkCase, error) {
	if strings.TrimSpace(selector) == "" {
		return append([]benchmarkCase(nil), cases...), nil
	}
	wanted := map[string]bool{}
	for _, value := range strings.Split(selector, ",") {
		if value = strings.TrimSpace(value); value != "" {
			wanted[value] = true
		}
	}
	out := make([]benchmarkCase, 0, len(cases))
	for _, item := range cases {
		if wanted[item.ID] || wanted[item.Category] {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("case filter %q matched no cases", selector)
	}
	return out, nil
}
