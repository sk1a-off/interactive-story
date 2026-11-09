package generation

import (
	"errors"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

// prepareIsolatedJournalEvidence is an opt-in experiment, not a general dependency
// resolver. Only existing-entry updates can survive bad evidence on new items.
// Unknown references, removals, duplicate identities and overlapping evidence
// retain all-or-nothing rejection. Domain validation still runs on the whole batch.
func prepareIsolatedJournalEvidence(beat string, current []generationtarget.JournalEntry, changes []journalChange) ([]journalChange, error) {
	prepared, err := prepareJournalEvidence(beat, current, changes)
	if err == nil || !errors.Is(err, ErrRejectedProposal) {
		return prepared, err
	}
	if len(changes) > 8 {
		return nil, ErrRejectedProposal
	}
	byID := map[string]generationtarget.JournalEntry{}
	names := map[string]bool{}
	key := func(category, name string) string {
		return strings.ToLower(strings.TrimSpace(category)) + "|" + strings.ToLower(strings.TrimSpace(name))
	}
	for _, entry := range current {
		byID[entry.ID] = entry
		names[key(entry.Category, entry.Name)] = true
	}
	seenIDs := map[string]bool{}
	seenNames := map[string]bool{}
	var good []journalChange
	var bad []journalChange
	for _, change := range changes {
		switch strings.ToLower(strings.TrimSpace(change.Operation)) {
		case "update":
			entryID := strings.TrimSpace(change.EntryID)
			base, ok := byID[entryID]
			if !ok || base.Status != "active" || seenIDs[entryID] {
				return nil, ErrRejectedProposal
			}
			seenIDs[entryID] = true
			// Renames may alias another change; do not attempt to resolve them here.
			if change.Name != "" && change.Name != base.Name {
				return nil, ErrRejectedProposal
			}
			if !evidenceBelongsToBeat(beat, change.EvidenceQuote) {
				return nil, ErrRejectedProposal
			}
			good = append(good, change)
		case "create":
			nameKey := key(change.Category, change.Name)
			if strings.TrimSpace(change.EntryID) != "" || strings.ToLower(strings.TrimSpace(change.Category)) != "item" || strings.TrimSpace(change.Name) == "" || names[nameKey] || seenNames[nameKey] {
				return nil, ErrRejectedProposal
			}
			seenNames[nameKey] = true
			if evidenceBelongsToBeat(beat, change.EvidenceQuote) {
				good = append(good, change)
			} else {
				bad = append(bad, change)
			}
		default:
			return nil, ErrRejectedProposal
		}
	}
	// Only one existing currency balance can be salvaged. Multiple balances or
	// item mutations may represent a transfer, exchange, crafting, or consumption.
	if len(bad) == 0 || len(good) != 1 || strings.ToLower(strings.TrimSpace(good[0].Operation)) != "update" || byID[strings.TrimSpace(good[0].EntryID)].Category != "currency" {
		return nil, ErrRejectedProposal
	}
	for _, valid := range good {
		for _, invalid := range bad {
			a, b := normalizeEvidenceText(valid.EvidenceQuote), normalizeEvidenceText(invalid.EvidenceQuote)
			// Empty evidence offers no dependency information, so fail closed.
			if b == "" || strings.Contains(a, b) || strings.Contains(b, a) {
				return nil, ErrRejectedProposal
			}
		}
	}
	return prepareJournalEvidence(beat, current, good)
}
