package main

import (
	"fmt"
	"net/http"
	"strconv"
)

// Conservative local guard after the 2026-11-12 browser probe, not a model limit.
const defaultTextCharacters = 65536

func textCharacterLimit(raw string) (int, error) {
	if raw == "" {
		return defaultTextCharacters, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1024 || n > 1048576 {
		return 0, fmt.Errorf("CHATGPT_BRIDGE_MAX_TEXT_CHARS must be 1024..1048576")
	}
	return n, nil
}

func utf16Characters(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}

func rejectTextSize(w http.ResponseWriter, actual, limit int) {
	w.Header().Set("X-Story-Bridge-Limit-Scope", "text_characters")
	w.Header().Set("X-Story-Bridge-Request-Characters", strconv.Itoa(actual))
	w.Header().Set("X-Story-Bridge-Limit-Characters", strconv.Itoa(limit))
	http.Error(w, "local text-only safety limit exceeded; reduce context; no file fallback", http.StatusRequestEntityTooLarge)
}
