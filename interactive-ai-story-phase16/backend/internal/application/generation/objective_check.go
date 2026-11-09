package generation

import (
	"encoding/json"

	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

// CheckObjectiveProposal runs production evidence and domain gates without LLM
// repair or persistence. Intended for offline evaluation of frozen responses.
func CheckObjectiveProposal(beat string, current []generationtarget.Objective, raw json.RawMessage) (json.RawMessage, error) {
	var proposal struct {
		Changes []objectiveChange `json:"changes"`
	}
	if json.Unmarshal(raw, &proposal) != nil || proposal.Changes == nil {
		return nil, ErrMalformedProposal
	}
	changes, err := prepareObjectiveEvidence(beat, current, proposal.Changes)
	if err != nil {
		return nil, err
	}
	_, normalized, err := validateObjectiveChanges(current, changes)
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}
