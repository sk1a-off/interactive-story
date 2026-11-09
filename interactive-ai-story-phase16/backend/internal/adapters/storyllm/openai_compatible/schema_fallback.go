package openai_compatible

import (
	"errors"
	"net/http"
	"strings"
)

var errSchemaUnsupported = errors.New("provider explicitly rejected response schema")

// Never mask authentication, quota, transport, server or malformed-schema errors.
// Raw provider text is used only for classification and is never retained.
func schemaUnsupported(status int, raw []byte) bool {
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		return false
	}
	text := strings.ToLower(string(raw))
	if !strings.Contains(text, "schema") && !strings.Contains(text, "response_format") && !strings.Contains(text, "responseformat") {
		return false
	}
	for _, marker := range []string{"not supported", "unsupported", "unknown field", "unknown name", "unrecognized", "not allowed", "does not support"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
