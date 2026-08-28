package narrative

import (
	"math"
	"regexp"
	"strings"
)

var sentencePattern = regexp.MustCompile(`[^.!?…]+(?:[.!?…]+[»”"'’)]*|$)`)

// DisplayParagraphs creates stable Reader paragraphs without mutating Canon.
// Explicit blank-line paragraphs win. If a local model ignores blank lines,
// long single-block prose is split into 7-9 readable display paragraphs.
// Image anchors use this same function, so placement is deterministic.
func DisplayParagraphs(text string) []string {
	normalized := strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if normalized == "" {
		return nil
	}

	explicitRaw := regexp.MustCompile(`\n\s*\n`).Split(normalized, -1)
	explicit := make([]string, 0, len(explicitRaw))
	for _, part := range explicitRaw {
		if v := strings.TrimSpace(part); v != "" {
			explicit = append(explicit, v)
		}
	}
	if len(explicit) >= 4 {
		return explicit
	}

	matches := sentencePattern.FindAllString(normalized, -1)
	sentences := make([]string, 0, len(matches))
	for _, match := range matches {
		if v := strings.TrimSpace(match); v != "" {
			sentences = append(sentences, v)
		}
	}
	if len(sentences) < 8 {
		if len(explicit) > 0 {
			return explicit
		}
		return []string{normalized}
	}

	target := int(math.Ceil(float64(len(sentences)) / 5.0))
	if target < 7 {
		target = 7
	}
	if target > 9 {
		target = 9
	}

	out := make([]string, 0, target)
	cursor := 0
	for i := 0; i < target; i++ {
		remaining := len(sentences) - cursor
		groupsLeft := target - i
		take := int(math.Ceil(float64(remaining) / float64(groupsLeft)))
		if take < 1 {
			take = 1
		}
		end := cursor + take
		if end > len(sentences) {
			end = len(sentences)
		}
		if cursor < end {
			out = append(out, strings.Join(sentences[cursor:end], " "))
		}
		cursor = end
	}
	return out
}
