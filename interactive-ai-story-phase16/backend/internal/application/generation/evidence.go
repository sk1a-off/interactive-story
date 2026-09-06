package generation

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

// normalizeEvidenceText deliberately normalizes only representation. It does
// not stem, translate, or paraphrase: evidence must remain a verbatim excerpt
// of the newly generated beat rather than a claim copied from older context.
func normalizeEvidenceText(value string) string {
	value = norm.NFKC.String(value)
	return strings.Join(strings.Fields(value), " ")
}

func evidenceBelongsToBeat(beat, quote string) bool {
	quote = normalizeEvidenceText(quote)
	return quote != "" && strings.Contains(normalizeEvidenceText(beat), quote)
}

func prepareWorldEvidence(beat string, changes []worldChange) ([]worldChange, error) {
	for index := range changes {
		if !evidenceBelongsToBeat(beat, changes[index].EvidenceQuote) {
			return nil, ErrRejectedProposal
		}
	}
	return changes, nil
}

func prepareJournalEvidence(beat string, current []generationtarget.JournalEntry, changes []journalChange) ([]journalChange, error) {
	byID := make(map[string]generationtarget.JournalEntry, len(current))
	for _, entry := range current {
		byID[entry.ID] = entry
	}
	out := make([]journalChange, 0, len(changes))
	for _, change := range changes {
		if !evidenceBelongsToBeat(beat, change.EvidenceQuote) {
			return nil, ErrRejectedProposal
		}
		change.Evidence = normalizeEvidenceText(change.EvidenceQuote)
		if strings.EqualFold(strings.TrimSpace(change.Operation), "update") {
			if base, ok := byID[strings.TrimSpace(change.EntryID)]; ok && journalChangeIsNoop(base, change) {
				continue
			}
		}
		out = append(out, change)
	}
	return out, nil
}

func journalChangeIsNoop(base generationtarget.JournalEntry, change journalChange) bool {
	if change.Quantity != nil && *change.Quantity != base.Quantity {
		return false
	}
	if value := strings.TrimSpace(change.Name); value != "" && value != base.Name {
		return false
	}
	if value := strings.TrimSpace(change.Description); value != "" && value != base.Description {
		return false
	}
	if value := strings.TrimSpace(change.Level); value != "" && value != base.Level {
		return false
	}
	if value := strings.TrimSpace(change.Status); value != "" && value != base.Status {
		return false
	}
	return len(change.Tags) == 0 || sameFoldedStrings(change.Tags, base.Tags)
}

func prepareObjectiveEvidence(beat string, current []generationtarget.Objective, changes []objectiveChange) ([]objectiveChange, error) {
	byID := make(map[string]generationtarget.Objective, len(current))
	for _, objective := range current {
		byID[objective.ID] = objective
	}
	out := make([]objectiveChange, 0, len(changes))
	for _, change := range changes {
		if !evidenceBelongsToBeat(beat, change.EvidenceQuote) {
			return nil, ErrRejectedProposal
		}
		change.Evidence = normalizeEvidenceText(change.EvidenceQuote)
		base, existing := byID[strings.TrimSpace(change.ObjectiveID)]
		switch strings.ToLower(strings.TrimSpace(change.Operation)) {
		case "progress":
			if existing && change.Progress <= base.Progress {
				continue
			}
		case "complete":
			if existing && !quoteSupportsCriterion(change.Evidence, base.SuccessCriteria) {
				return nil, ErrRejectedProposal
			}
		}
		out = append(out, change)
	}
	return out, nil
}

// Completion is deliberately conservative. At least one meaningful word stem
// from the observable success criterion must be present in the verbatim quote.
// Ambiguous completions remain active instead of entering Canon prematurely.
func quoteSupportsCriterion(quote, criterion string) bool {
	quoteStems := meaningfulStems(quote)
	for stem := range meaningfulStems(criterion) {
		if _, ok := quoteStems[stem]; ok {
			return true
		}
	}
	return false
}

func meaningfulStems(value string) map[string]struct{} {
	var cleaned strings.Builder
	for _, char := range strings.ToLower(normalizeEvidenceText(value)) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			cleaned.WriteRune(char)
		} else {
			cleaned.WriteByte(' ')
		}
	}
	out := map[string]struct{}{}
	for _, word := range strings.Fields(cleaned.String()) {
		runes := []rune(word)
		if len(runes) < 5 {
			continue
		}
		out[string(runes[:min(5, len(runes))])] = struct{}{}
	}
	return out
}

func sameFoldedStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !strings.EqualFold(strings.TrimSpace(left[index]), strings.TrimSpace(right[index])) {
			return false
		}
	}
	return true
}
