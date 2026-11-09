package main

import (
	"encoding/json"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/domain/event"
)

type questProvenance struct {
	Kind           string     `json:"kind"`
	VerifiedQuote  bool       `json:"verifiedQuote"`
	SourceEventIDs []event.ID `json:"sourceEventIds,omitempty"`
	Rule           string     `json:"rule,omitempty"`
	Note           string     `json:"note"`
}

type ledgerObjective struct {
	ID       string `json:"objectiveId"`
	Parent   string `json:"parentObjectiveId"`
	Scope    string `json:"scope"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
	Evidence string `json:"evidence"`
}

func classifyQuestProvenance(e event.StoredEvent, earlier []event.StoredEvent) questProvenance {
	unknown := questProvenance{Kind: "unverified_legacy", Note: "Historical state only; not verified evidence of a new achievement. Keep source prose."}
	var p ledgerObjective
	if json.Unmarshal(e.Payload, &p) != nil {
		return unknown
	}
	quote := strings.Join(strings.Fields(p.Evidence), " ")
	if quote != "" && e.GenerationID != nil {
		for _, b := range earlier {
			if b.TimelineID != e.TimelineID || b.Seq >= e.Seq || b.Type != "beat_committed" || b.GenerationID == nil || *b.GenerationID != *e.GenerationID {
				continue
			}
			var beat struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(b.Payload, &beat) != nil {
				continue
			}
			if strings.Contains(strings.Join(strings.Fields(beat.Text), " "), quote) {
				return questProvenance{Kind: "direct_quote", VerifiedQuote: true, SourceEventIDs: []event.ID{b.ID}, Note: "Quote verified in the linked historical beat; do not count it again as new progress. Semantic sufficiency is not inferred."}
			}
		}
	}
	if quote == "" && p.Scope == "global" && p.Status == "active" && e.Type == "objective_updated" && e.GenerationID != nil {
		type state struct {
			p ledgerObjective
			e event.StoredEvent
		}
		latest := map[string]state{}
		for _, old := range earlier {
			if old.TimelineID != e.TimelineID || old.Seq >= e.Seq || (old.Type != "objective_created" && old.Type != "objective_updated") {
				continue
			}
			var v ledgerObjective
			if json.Unmarshal(old.Payload, &v) == nil {
				latest[v.ID] = state{v, old}
			}
		}
		parent, exists := latest[p.ID]
		total, count := 0, 0
		changed := false
		refs := []event.ID{}
		// Preserve deterministic source ordering, selecting latest child states.
		for _, old := range earlier {
			var v ledgerObjective
			if json.Unmarshal(old.Payload, &v) != nil {
				continue
			}
			last, ok := latest[v.ID]
			if !ok || last.e.ID != old.ID || v.Parent != p.ID {
				continue
			}
			count++
			if v.Status == "completed" {
				total += 100
			} else {
				total += v.Progress
			}
			refs = append(refs, old.ID)
			if old.GenerationID != nil && *old.GenerationID == *e.GenerationID {
				changed = true
			}
		}
		if exists && count > 0 && changed {
			calculated := total / count
			if calculated > 99 {
				calculated = 99
			}
			if p.Progress == calculated && calculated > parent.p.Progress && parent.p.Status == "active" {
				return questProvenance{Kind: "derived_progress_consistent", SourceEventIDs: append([]event.ID{parent.e.ID}, refs...), Rule: "min(99, floor(sum(completed child ? 100 : child progress) / child count)); increase only", Note: "Historical values match child aggregation; inferred origin, not an explicit recorded origin marker."}
			}
		}
	}
	if e.GenerationID == nil {
		return questProvenance{Kind: "initial_or_manual", Note: "No generation link; source does not distinguish setup from manual editing. Not an inferred achievement."}
	}
	return unknown
}
