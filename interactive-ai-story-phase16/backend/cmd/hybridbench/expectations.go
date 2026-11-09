package main

import (
	"encoding/json"
	"fmt"
)

// Pointer fields distinguish an explicit zero expectation from an omitted one.
type expectedMutation struct {
	EventType       string `json:"eventType"`
	EntityID        string `json:"entityId"`
	Status          string `json:"status,omitempty"`
	Quantity        *int   `json:"quantity,omitempty"`
	Progress        *int   `json:"progress,omitempty"`
	MinimumProgress *int   `json:"minimumProgress,omitempty"`
	MaximumProgress *int   `json:"maximumProgress,omitempty"`
}

func (e expectedMutation) validate() error {
	if !validEntityID(e.EntityID) {
		return fmt.Errorf("invalid expected entity ID %q", e.EntityID)
	}
	switch e.EventType {
	case "journal_entry_updated":
		if e.Quantity == nil || *e.Quantity < 0 || e.Progress != nil || e.MinimumProgress != nil || e.MaximumProgress != nil || (e.Status != "active" && e.Status != "inactive") {
			return fmt.Errorf("invalid journal expectation")
		}
	case "objective_updated":
		if e.Quantity != nil || (e.Status != "active" && e.Status != "completed" && e.Status != "failed") {
			return fmt.Errorf("invalid objective expectation")
		}
		if e.Progress != nil {
			if *e.Progress < 0 || *e.Progress > 100 || e.MinimumProgress != nil || e.MaximumProgress != nil {
				return fmt.Errorf("invalid exact objective progress expectation")
			}
		} else if e.MinimumProgress == nil || e.MaximumProgress == nil || *e.MinimumProgress < 0 || *e.MaximumProgress > 100 || *e.MinimumProgress > *e.MaximumProgress {
			return fmt.Errorf("invalid objective progress range")
		}
	default:
		return fmt.Errorf("unsupported expected mutation %q", e.EventType)
	}
	return nil
}

func (e expectedMutation) matches(events []eventResult) bool {
	if e.validate() != nil {
		return false
	}
	count := 0
	for _, event := range events {
		if event.Type != e.EventType {
			continue
		}
		var p struct {
			EntryID     string `json:"entryId"`
			ObjectiveID string `json:"objectiveId"`
			Status      string `json:"status"`
			Quantity    *int   `json:"quantity"`
			Progress    *int   `json:"progress"`
		}
		if json.Unmarshal(event.Payload, &p) != nil {
			return false
		}
		entity := p.EntryID
		if e.EventType == "objective_updated" {
			entity = p.ObjectiveID
		}
		if entity != e.EntityID {
			continue
		}
		count++
		if p.Status != e.Status || (e.Quantity != nil && (p.Quantity == nil || *p.Quantity != *e.Quantity)) || (e.Progress != nil && (p.Progress == nil || *p.Progress != *e.Progress)) {
			return false
		}
		if e.MinimumProgress != nil && (p.Progress == nil || *p.Progress < *e.MinimumProgress || *p.Progress > *e.MaximumProgress) {
			return false
		}
	}
	return count == 1
}
