package generation

import (
	"encoding/json"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

// CheckJournalProposal applies production gates without persistence or model repair.
func CheckJournalProposal(beat string, current []generationtarget.JournalEntry, raw json.RawMessage) (json.RawMessage, error) {
	return checkJournalProposal(beat, current, raw, false)
}

// CheckIsolatedJournalProposal evaluates the experimental opt-in gate without writes.
func CheckIsolatedJournalProposal(beat string, current []generationtarget.JournalEntry, raw json.RawMessage) (json.RawMessage, error) {
	return checkJournalProposal(beat, current, raw, true)
}

func checkJournalProposal(beat string, current []generationtarget.JournalEntry, raw json.RawMessage, isolate bool) (json.RawMessage, error) {
	var proposal struct {
		Changes []journalChange `json:"changes"`
	}
	if json.Unmarshal(raw, &proposal) != nil || proposal.Changes == nil {
		return nil, ErrMalformedProposal
	}
	changes, err := prepareJournalEvidence(beat, current, proposal.Changes)
	if isolate {
		changes, err = prepareIsolatedJournalEvidence(beat, current, proposal.Changes)
	}
	if err != nil {
		return nil, err
	}
	_, normalized, err := validateJournalChanges(current, changes)
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}
