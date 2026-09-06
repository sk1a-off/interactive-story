package generation

import (
	"encoding/json"

	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

// contextWithout removes only fields that are supplied to a role separately.
// It is a lossless representation change: no historical coverage is reduced.
func contextWithout(target generationtarget.Target, fields ...string) map[string]any {
	raw, _ := json.Marshal(target)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	for _, field := range fields {
		delete(out, field)
	}
	return out
}
