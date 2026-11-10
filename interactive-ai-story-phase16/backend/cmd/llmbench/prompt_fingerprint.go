package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
)

// Revision's public JSON intentionally omits all fields. Hash actual content
// explicitly; marshaling Revision itself hashes the constant empty object.
func promptFingerprint(revision promptset.Revision) string {
	raw, _ := json.Marshal(struct {
		Prompts  map[string]string                 `json:"prompts"`
		Settings map[string]promptset.RoleSettings `json:"roleSettings"`
	}{revision.Prompts, revision.RoleSettings})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
