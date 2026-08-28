package generation

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	ErrRepetitiveNarrative = errors.New("generated narrative repeats previous prose")
	wordPattern            = regexp.MustCompile(`[\p{L}\p{N}]+`)
	sentenceBoundary       = regexp.MustCompile(`[.!?…]+[»”"']*|\n+`)
)

func narrativeNoveltyError(previous, candidate string) error {
	previousWords := normalizedWords(previous)
	candidateWords := normalizedWords(candidate)
	if len(previousWords) == 0 || len(candidateWords) == 0 {
		return nil
	}

	// Any long verbatim sequence is a copy even when it sits inside an otherwise
	// rewritten paragraph. Eight words is long enough to avoid ordinary scene
	// vocabulary while catching copied dialogue and narration reliably.
	previousEight := shingles(previousWords, 8)
	for phrase := range shingles(candidateWords, 8) {
		if _, exists := previousEight[phrase]; exists {
			return fmt.Errorf("%w: copied phrase %q", ErrRepetitiveNarrative, phrase)
		}
	}

	previousFour := shingles(previousWords, 4)
	candidateFour := shingles(candidateWords, 4)
	if overlapRatio(previousFour, candidateFour) >= 0.18 {
		return fmt.Errorf("%w: excessive four-word overlap", ErrRepetitiveNarrative)
	}

	seenSentences := map[string]struct{}{}
	for _, sentence := range splitNormalizedSentences(candidate) {
		if len(strings.Fields(sentence)) < 10 {
			continue
		}
		if _, exists := seenSentences[sentence]; exists {
			return fmt.Errorf("%w: repeated sentence inside draft", ErrRepetitiveNarrative)
		}
		seenSentences[sentence] = struct{}{}
	}
	paragraphs := nonEmptyDraftParagraphs(candidate)
	for index := range paragraphs {
		for previousIndex := 0; previousIndex < index; previousIndex++ {
			if paragraphShingleOverlap(paragraphs[index], paragraphs[previousIndex]) >= 0.18 {
				return fmt.Errorf("%w: repeated or paraphrased paragraph inside draft", ErrRepetitiveNarrative)
			}
		}
	}
	return nil
}

func nonEmptyDraftParagraphs(text string) []string {
	parts := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func paragraphShingleOverlap(left, right string) float64 {
	leftShingles := shingles(normalizedWords(left), 3)
	rightShingles := shingles(normalizedWords(right), 3)
	if len(leftShingles) == 0 || len(rightShingles) == 0 {
		return 0
	}
	matched := 0
	for phrase := range leftShingles {
		if _, exists := rightShingles[phrase]; exists {
			matched++
		}
	}
	denominator := min(len(leftShingles), len(rightShingles))
	return float64(matched) / float64(denominator)
}

func normalizedWords(text string) []string {
	raw := wordPattern.FindAllString(strings.ToLower(text), -1)
	words := make([]string, 0, len(raw))
	for _, word := range raw {
		if word = strings.TrimSpace(word); word != "" {
			words = append(words, word)
		}
	}
	return words
}

func shingles(words []string, size int) map[string]struct{} {
	out := map[string]struct{}{}
	if size < 1 || len(words) < size {
		return out
	}
	for i := 0; i+size <= len(words); i++ {
		out[strings.Join(words[i:i+size], " ")] = struct{}{}
	}
	return out
}

func overlapRatio(reference, candidate map[string]struct{}) float64 {
	if len(candidate) == 0 {
		return 0
	}
	matched := 0
	for phrase := range candidate {
		if _, exists := reference[phrase]; exists {
			matched++
		}
	}
	return float64(matched) / float64(len(candidate))
}

func splitNormalizedSentences(text string) []string {
	parts := sentenceBoundary.Split(text, -1)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if words := normalizedWords(part); len(words) > 0 {
			out = append(out, strings.Join(words, " "))
		}
	}
	return out
}
