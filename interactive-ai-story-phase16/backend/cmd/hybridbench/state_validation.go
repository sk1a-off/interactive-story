package main

import (
	"encoding/json"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

// State validators check emitted mutations independently of Pipeline policy.
// They do not measure recall: an empty mutation set satisfies these invariants.
func validJournalState(events []eventResult, initial []generationtarget.JournalEntry) bool {
	state := map[string]generationtarget.JournalEntry{}
	for _, entry := range initial {
		state[entry.ID] = entry
	}
	for _, event := range events {
		if event.Type != "journal_entry_created" && event.Type != "journal_entry_updated" {
			continue
		}
		var value struct {
			generationtarget.JournalEntry
			EntryID  string `json:"entryId"`
			Quantity *int   `json:"quantity"`
		}
		if json.Unmarshal(event.Payload, &value) != nil || !validEntityID(value.EntryID) || strings.TrimSpace(value.Name) == "" {
			return false
		}
		base, exists := state[value.EntryID]
		if event.Type == "journal_entry_created" {
			if exists || value.Status != "active" {
				return false
			}
		} else if !exists || base.Status != "active" || base.Category != value.Category {
			return false
		}
		if value.Status != "active" && value.Status != "inactive" {
			return false
		}
		switch value.Category {
		case "item", "currency":
			if value.Quantity == nil || *value.Quantity < 0 {
				return false
			}
			if value.Status == "inactive" && *value.Quantity != 0 {
				return false
			}
			if value.Status == "active" && value.Category == "item" && *value.Quantity < 1 {
				return false
			}
			limit := 9999
			if value.Category == "currency" {
				limit = 999999999
			}
			if *value.Quantity > limit {
				return false
			}
		case "ability", "attribute":
		default:
			return false
		}
		entry := value.JournalEntry
		entry.ID = value.EntryID
		if value.Quantity != nil {
			entry.Quantity = *value.Quantity
		}
		state[entry.ID] = entry
	}
	return true
}

func validObjectiveState(events []eventResult, initial []generationtarget.Objective) bool {
	state := map[string]generationtarget.Objective{}
	for _, objective := range initial {
		if objective.ID != "" {
			state[objective.ID] = objective
		}
	}
	for _, event := range events {
		if event.Type != "objective_created" && event.Type != "objective_updated" {
			continue
		}
		var value struct {
			generationtarget.Objective
			ObjectiveID string `json:"objectiveId"`
			Progress    *int   `json:"progress"`
		}
		if json.Unmarshal(event.Payload, &value) != nil || !validEntityID(value.ObjectiveID) || value.Progress == nil || strings.TrimSpace(value.SuccessCriteria) == "" {
			return false
		}
		progress := *value.Progress
		if progress < 0 || progress > 100 {
			return false
		}
		switch value.Status {
		case "active", "failed":
			if progress == 100 {
				return false
			}
		case "completed":
			if progress != 100 {
				return false
			}
		default:
			return false
		}
		base, exists := state[value.ObjectiveID]
		if event.Type == "objective_created" {
			if exists || value.Status != "active" {
				return false
			}
		} else {
			if !exists || base.Status != "active" || progress < base.Progress || value.Scope != base.Scope || value.ParentObjectiveID != base.ParentObjectiveID {
				return false
			}
			if value.Status == "failed" && progress != base.Progress {
				return false
			}
		}
		objective := value.Objective
		objective.ID, objective.Progress = value.ObjectiveID, progress
		state[objective.ID] = objective
	}
	return true
}

func validEntityID(value string) bool {
	parsed, err := id.Parse(value)
	return err == nil && !parsed.IsZero()
}

// For negative fixtures only: no objective may become completed/failed, even
// if a model creates a different objective instead of updating the expected ID.
func objectivesRemainActive(events []eventResult, initial []generationtarget.Objective) bool {
	if len(initial) == 0 || !validObjectiveState(events, initial) {
		return false
	}
	for _, objective := range initial {
		if objective.Status != "active" {
			return false
		}
	}
	for _, event := range events {
		if event.Type != "objective_created" && event.Type != "objective_updated" {
			continue
		}
		var p struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(event.Payload, &p) != nil || p.Status != "active" {
			return false
		}
	}
	return true
}

// Negative controls permit omission or unchanged updates, never new progress
// on an existing goal or a rewritten criterion. New stages are not progress.
func objectiveProgressUnchanged(events []eventResult, initial []generationtarget.Objective) bool {
	if !objectivesRemainActive(events, initial) {
		return false
	}
	for _, event := range events {
		if event.Type != "objective_updated" {
			continue
		}
		var value struct {
			ObjectiveID     string `json:"objectiveId"`
			Progress        int    `json:"progress"`
			SuccessCriteria string `json:"successCriteria"`
		}
		if json.Unmarshal(event.Payload, &value) != nil {
			return false
		}
		for _, base := range initial {
			if value.ObjectiveID == base.ID && (value.Progress != base.Progress || value.SuccessCriteria != base.SuccessCriteria) {
				return false
			}
		}
	}
	return true
}
