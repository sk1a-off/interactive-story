package promptset

import (
	"testing"

	domain "github.com/local/interactive-ai-story/backend/internal/domain/promptset"
)

func TestCompleteRevisionAddsNewBuiltinRolesWithoutChangingExistingPrompts(t *testing.T) {
	builtin, err := BuiltinCommand()
	if err != nil {
		t.Fatal(err)
	}
	prompts := map[string]string{}
	settings := map[string]domain.RoleSettings{}
	for _, role := range domain.Roles {
		if role == "setup_assistant" {
			continue
		}
		prompts[role] = builtin.Prompts[role]
		settings[role] = builtin.RoleSettings[role]
	}
	prompts["writer"] = "custom writer prompt"

	cmd, upgraded, err := completeRevision(domain.Revision{Prompts: prompts, RoleSettings: settings})
	if err != nil {
		t.Fatal(err)
	}
	if !upgraded {
		t.Fatal("historical revision missing setup_assistant should be upgraded")
	}
	if cmd.Prompts["writer"] != "custom writer prompt" {
		t.Fatal("existing customized prompt was overwritten")
	}
	if cmd.Prompts["setup_assistant"] == "" {
		t.Fatal("new setup_assistant builtin prompt was not added")
	}
	if err = cmd.Validate(); err != nil {
		t.Fatalf("upgraded revision must be complete: %v", err)
	}
}
