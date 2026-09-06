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
	"quest_evaluator": `# Focused quest journal and evidence compatibility v2
- A newly created quest or stage must be active and unresolved. Never create an already completed or failed historical objective.
- Never maintain more than 5 active quest lines total, more than 3 active main or side quest lines of one type, or more than 3 active stages inside one quest.
- Prefer progressing, completing or extending an existing matching quest over creating a near-duplicate.
- Every change must include evidenceQuote copied verbatim from newBeat. Action text, previous beats and older context are not evidence.
- Complete or fail only when that quote directly demonstrates the current observable success or failure criterion; otherwise keep the objective active.`,
	"state_evaluator": `# New-beat evidence compatibility v1
- Every change must include evidenceQuote copied verbatim from newBeat. Action text, previous beats and older context are not evidence.
- Never emit a no-op update whose durable fields equal the current journal entry.`,
	"world_evaluator": `# New-beat evidence compatibility v1
- Every character or location change must include evidenceQuote copied verbatim from newBeat. Action text, previous beats and older context are not evidence.
- Avoid no-op updates and entities introduced only as incidental decoration.`,
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
	changed := removeDeprecatedWorldRuleInstructions(prompts)
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

// removeDeprecatedWorldRuleInstructions migrates immutable prompt revisions
// created while the separate world-rules subsystem was active. Only the exact
// feature-owned fragments are removed, so unrelated Prompt Studio edits stay
// intact. The unused lore_guard role remains in stored revisions for schema
// compatibility, but the generation pipeline no longer calls it.
func removeDeprecatedWorldRuleInstructions(prompts map[string]string) bool {
	fragments := map[string][]string{
		"director": {
			"- Treat supplied `worldRules` and `feasibility` as outcome boundaries. A hard rule cannot be overridden by dramatic convenience. If an attempted action violates a prerequisite or lacks a resource, direct a failure, partial result, discovery or meaningful cost instead.\n- Hidden rules may shape consequences but must not be revealed to the protagonist before evidence appears in the story.",
		},
		"writer": {
			"- Obey every supplied established `worldRules` card. Apply its prerequisites, energy source, costs, limits, progression and exceptions exactly; never let the neural interface create energy or grant an unlearned ability unless an explicit rule permits it.\n- `feasibility` is authoritative for possible outcomes. Player intent is an attempt, so failure and partial success are preferable to breaking a hard law.\n- Hidden or mystery rules may affect events but must not be explained to the protagonist without observable discovery.\n- When `loreViolations` and `rejectedDraft` are supplied, replace the whole draft once and repair every cited violation without recapping the previous beat.",
		},
		"setup_architect": {
			"- `systems`: only the world systems and their resource definitions; do not write rules yet.\n- `laws`: rules referring to system IDs from `canon.worldSystems`, plus a concise glossary; do not redefine systems.\n",
			"- `world_rules`: `systems`, `rules`, and `glossary`. Systems have stable lowercase `id`, `name`, `kind`, `description`, and `resources`. Rules have stable addressable `id`, `systemId`, `title`, `category`, `severity` (`hard`, `soft`, `mystery`, or `belief`), testable `statement`, arrays `preconditions`, `costs`, `forbiddenResults`, `exceptions`, `tags`, `visibility`, and `status`. A neural interface must distinguish computation/control from energy capacity. Every power system must state its source, capabilities, limits, costs, failure modes and progression.\n",
		},
		"world_evaluator": {
			`,"ruleChanges":[
  {"operation":"reveal","ruleId":"exact existing rule id","evidence":"new beat evidence"},
  {"operation":"propose_rule","systemId":"exact system id","title":"...","category":"mechanism","severity":"soft","statement":"...","preconditions":[],"costs":[],"forbiddenResults":[],"exceptions":[],"tags":[],"visibility":"canon_only","evidence":"new beat evidence"},
  {"operation":"add_exception","systemId":"exact system id","exceptionOf":"exact base rule id","title":"...","severity":"soft","statement":"...","preconditions":[],"costs":[],"tags":[],"visibility":"canon_only","evidence":"new beat evidence"}
],"resourceChanges":[{"resourceId":"exact existing resource id","delta":-1,"evidence":"observable use in the new beat"}]`,
			"- Established rules are axioms. Never rewrite, delete, weaken or supersede them automatically.\n- `reveal` changes only what the hero knows about an existing hidden rule and requires explicit discovery in the new beat.\n- A genuinely new inferred law or exception is only a proposal. In safe mode it remains pending for Director approval; a new hard law is always pending.\n- Resource deltas must be directly caused by the new beat. Never exceed supplied min/max bounds and never manufacture energy to make an action succeed.\n- Return empty `ruleChanges` and `resourceChanges` arrays when the beat establishes no durable rule discovery or resource change.",
		},
		"director_editor": {
			", `worldCanon` (systems, addressable rules, resource state)",
			"- `upsert_world_system`: create or update one system with `systemId`, `name`, `kind`, `description`, `resources`, `status`. Preserve a stable textual `systemId`.\n- `upsert_world_rule`: create, edit or approve one rule. Use `status: pending` when the user asked for an idea, and `status: established` only when the user explicitly accepts it as canon.\n- `archive_world_rule`: payload contains only `ruleId`. Never silently delete a law that existing prose may depend on.\n- `update_world_resource`: update one existing bounded resource with its complete current payload and a new `currentValue`.\n",
			"World rule payload:\n`ruleId`, `systemId`, `title`, `category` (`axiom`, `law`, `mechanism`, `limit`, `cost`, `progression`, `exception`, `social`, `terminology`), `severity` (`hard`, `soft`, `mystery`, `belief`), `statement`, arrays `preconditions`, `costs`, `forbiddenResults`, `exceptions`, `tags`, `visibility` (`canon_only`, `known_to_hero`, `public`, `hidden`), `status` (`established`, `pending`, `superseded`, `archived`), optional `exceptionOf`, and concrete `evidence`.\n\n",
			"9. A new hard law, exception, or retroactive change must remain pending unless the user's instruction explicitly says to approve/fix it as canon. Never rewrite unrelated rules.\n",
		},
	}
	changed := false
	for role, roleFragments := range fragments {
		for _, fragment := range roleFragments {
			if strings.Contains(prompts[role], fragment) {
				prompts[role] = strings.ReplaceAll(prompts[role], fragment, "")
				changed = true
			}
		}
		prompts[role] = strings.TrimSpace(prompts[role])
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
