package bootstrap

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	promptcatalog "github.com/local/interactive-ai-story/backend/prompts"
)

var promptUpgradeAddenda = map[string]string{
	"writer": `# Scene and chapter lifecycle compatibility v1
- Treat context.recentBeats as read-only story history and continue after context.previousBeatText without recapping or replaying completed prose.
- Follow pacing.transition exactly. For new_scene or new_chapter, begin at the first changed moment in the new time, place, participants or dramatic situation; make the transition clear in prose without printing structural labels or metadata.
- A structural transition never guarantees that the player's attempted action succeeded.`,
	"quest_evaluator": `# Focused quest journal compatibility v1
- A newly created quest or stage must be active and unresolved. Never create an already completed or failed historical objective.
- Never maintain more than 5 active quest lines total, more than 3 active main or side quest lines of one type, or more than 3 active stages inside one quest.
- Prefer progressing, completing or extending an existing matching quest over creating a near-duplicate.`,
}

func builtinPromptSet() (map[string]string, map[string]promptset.RoleSettings, error) {
	prompts := make(map[string]string, len(promptset.Roles))
	settings := make(map[string]promptset.RoleSettings, len(promptset.Roles))
	for _, role := range promptset.Roles {
		text, err := promptcatalog.Get("v1", role)
		if err != nil {
			return nil, nil, err
		}
		prompts[role] = text
		settings[role] = promptset.DefaultRoleSettings(role)
	}
	return prompts, settings, nil
}

// EnsurePromptSet guarantees that an active prompt-set revision exists and that
// it contains every prompt role known by the running binary. When a deployment
// introduces a new role, historical revisions stay immutable: bootstrap clones
// the active revision, fills only missing roles from builtin defaults and moves
// the active pointer to that new revision.
func EnsurePromptSet(ctx context.Context, p *pgxpool.Pool) error {
	builtinPrompts, builtinSettings, err := builtinPromptSet()
	if err != nil {
		return err
	}

	var count int
	if err = p.QueryRow(ctx, `SELECT count(*) FROM prompt_set_revisions`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return insertAndActivatePromptSet(ctx, p, builtinPrompts, builtinSettings)
	}

	var activeID id.ID
	err = p.QueryRow(ctx, `SELECT prompt_set_revision_id FROM prompt_active_set WHERE singleton=true`).Scan(&activeID)
	if err == pgx.ErrNoRows {
		if err = p.QueryRow(ctx, `SELECT id FROM prompt_set_revisions ORDER BY revision DESC LIMIT 1`).Scan(&activeID); err != nil {
			return err
		}
		if _, err = p.Exec(ctx, `INSERT INTO prompt_active_set(singleton,prompt_set_revision_id,updated_at)
VALUES(true,$1,now()) ON CONFLICT(singleton) DO UPDATE SET prompt_set_revision_id=EXCLUDED.prompt_set_revision_id,updated_at=now()`, activeID); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	var pRaw, sRaw []byte
	if err = p.QueryRow(ctx, `SELECT prompts,role_settings FROM prompt_set_revisions WHERE id=$1`, activeID).Scan(&pRaw, &sRaw); err != nil {
		return err
	}
	prompts := map[string]string{}
	settings := map[string]promptset.RoleSettings{}
	if err = json.Unmarshal(pRaw, &prompts); err != nil {
		return err
	}
	if err = json.Unmarshal(sRaw, &settings); err != nil {
		return err
	}

	changed := false
	for _, role := range promptset.Roles {
		if strings.TrimSpace(prompts[role]) == "" {
			prompts[role] = builtinPrompts[role]
			changed = true
		}
		if _, ok := settings[role]; !ok {
			settings[role] = builtinSettings[role]
			changed = true
		}
	}
	if applyPromptUpgrades(prompts) {
		changed = true
	}
	if !changed {
		return nil
	}
	return insertAndActivatePromptSet(ctx, p, prompts, settings)
}

func applyPromptUpgrades(prompts map[string]string) bool {
	changed := false
	for role, addendum := range promptUpgradeAddenda {
		marker := strings.SplitN(addendum, "\n", 2)[0]
		if strings.Contains(prompts[role], marker) {
			continue
		}
		prompts[role] = strings.TrimSpace(prompts[role]) + "\n\n" + addendum
		changed = true
	}
	return changed
}

func insertAndActivatePromptSet(ctx context.Context, p *pgxpool.Pool, prompts map[string]string, settings map[string]promptset.RoleSettings) error {
	cmd := promptset.CreateCommand{Prompts: prompts, RoleSettings: settings}
	if err := cmd.Validate(); err != nil {
		return err
	}
	pRaw, err := json.Marshal(prompts)
	if err != nil {
		return err
	}
	sRaw, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	tx, err := p.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var revisionID id.ID
	if err = tx.QueryRow(ctx, `INSERT INTO prompt_set_revisions(prompts,role_settings) VALUES($1,$2) RETURNING id`, pRaw, sRaw).Scan(&revisionID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO prompt_active_set(singleton,prompt_set_revision_id,updated_at)
VALUES(true,$1,now()) ON CONFLICT(singleton) DO UPDATE SET prompt_set_revision_id=EXCLUDED.prompt_set_revision_id,updated_at=now()`, revisionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
