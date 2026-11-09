package generation

import (
	"encoding/json"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

// CheckWorldProposal runs production gates without persistence or repair.
func CheckWorldProposal(beat string, target generationtarget.Target, raw json.RawMessage) (json.RawMessage, error) {
	var proposal struct {
		Changes []worldChange `json:"changes"`
	}
	if json.Unmarshal(raw, &proposal) != nil || proposal.Changes == nil {
		return nil, ErrMalformedProposal
	}
	changes, err := prepareWorldEvidence(beat, proposal.Changes)
	if err != nil {
		return nil, err
	}
	_, normalized, err := validateWorldChanges(target, changes)
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}
