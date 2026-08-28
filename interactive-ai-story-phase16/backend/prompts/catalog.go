package prompts

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed v1/*.md
var files embed.FS

func Get(version, role string) (string, error) {
	version = strings.TrimSpace(version)
	role = strings.TrimSpace(role)
	if version == "" {
		version = "v1"
	}
	if version != "v1" || role == "" {
		return "", fmt.Errorf("unsupported prompt %s/%s", version, role)
	}
	raw, e := files.ReadFile(version + "/" + role + ".md")
	if e != nil {
		return "", fmt.Errorf("load prompt %s/%s: %w", version, role, e)
	}
	return strings.TrimSpace(string(raw)), nil
}
