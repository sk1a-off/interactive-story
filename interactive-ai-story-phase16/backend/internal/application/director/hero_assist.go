package director

import (
	"encoding/json"
	"math"
	"strings"

	d "github.com/local/interactive-ai-story/backend/internal/domain/director"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/directorrepo"
)

func normalizeHeroAssist(result *AssistResult, view directorrepo.View) error {
	var hero map[string]any
	for _, row := range view.Characters {
		if row["kind"] == "player" {
			if hero != nil {
				return ErrInvalidAssistProposal
			}
			hero = row
		}
	}
	if hero == nil {
		return ErrInvalidAssistProposal
	}
	heroID, _ := hero["id"].(string)
	if parsed, err := id.Parse(heroID); err != nil || parsed.IsZero() {
		return ErrInvalidAssistProposal
	}
	journal := append(append(append([]map[string]any{}, view.Abilities...), view.Attributes...), view.Inventory...)
	seen := map[string]bool{}
	newNames := map[string]bool{}
	for index := range result.Operations {
		op := &result.Operations[index]
		var patch map[string]any
		if json.Unmarshal(op.Payload, &patch) != nil || len(patch) == 0 || op.Reference != "" || op.ParentReference != "" {
			return ErrInvalidAssistProposal
		}
		var before, after map[string]any
		var target string
		switch op.Type {
		case d.UpdateCharacterState:
			if !onlyHeroKeys(patch, "characterId", "mood", "currentGoal", "active") || (patch["characterId"] != nil && patch["characterId"] != heroID) {
				return ErrInvalidAssistProposal
			}
			before = map[string]any{"characterId": heroID, "mood": heroString(hero["mood"]), "currentGoal": heroString(hero["goal"]), "active": hero["active"] != false}
			after = mergeHeroPatch(before, patch)
			if _, ok := after["active"].(bool); !ok {
				return ErrInvalidAssistProposal
			}
			for _, key := range []string{"mood", "currentGoal"} {
				if _, ok := after[key].(string); !ok {
					return ErrInvalidAssistProposal
				}
			}
			target = "hero:" + heroID
		case d.SetStat:
			if !onlyHeroKeys(patch, "ownerType", "ownerId", "key", "value") || (patch["ownerId"] != nil && patch["ownerId"] != heroID) || (patch["ownerType"] != nil && patch["ownerType"] != "character") {
				return ErrInvalidAssistProposal
			}
			key, ok := patch["key"].(string)
			if !ok || strings.TrimSpace(key) == "" {
				return ErrInvalidAssistProposal
			}
			value, ok := patch["value"].(float64)
			if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
				return ErrInvalidAssistProposal
			}
			before = map[string]any{}
			after = map[string]any{"ownerType": "character", "ownerId": heroID, "key": key, "valueType": "number", "evolutionMode": "immediate", "value": value, "targetValue": nil}
			for _, row := range view.Stats {
				if row["ownerId"] == heroID && row["ownerType"] == "character" && row["key"] == key {
					if row["valueType"] != "number" {
						return ErrInvalidAssistProposal
					}
					for _, field := range []string{"ownerType", "ownerId", "key", "valueType", "value", "targetValue", "evolutionMode"} {
						before[field] = row[field]
					}
					after = mergeHeroPatch(before, map[string]any{"value": value})
				}
			}
			target = "stat:" + key
		case d.UpsertJournalEntry:
			if !onlyHeroKeys(patch, "entryId", "category", "name", "description", "quantity", "level", "status", "evidence", "tags") {
				return ErrInvalidAssistProposal
			}
			entryID := heroString(patch["entryId"])
			if value, exists := patch["entryId"]; exists {
				if _, ok := value.(string); !ok {
					return ErrInvalidAssistProposal
				}
			}
			before = map[string]any{}
			defaults := map[string]any{"description": "", "quantity": 0, "level": "", "status": "active", "evidence": "Предложено помощником режиссёра", "tags": []string{}}
			if entryID != "" {
				found := false
				for _, row := range journal {
					if row["id"] == entryID {
						found = true
						for _, key := range []string{"category", "name", "description", "quantity", "level", "status", "evidence", "tags"} {
							if row[key] != nil {
								defaults[key] = row[key]
							}
						}
					}
				}
				if !found {
					return ErrInvalidAssistProposal
				}
				defaults["entryId"] = entryID
				before = mergeHeroPatch(defaults, nil)
				if patch["category"] != nil && patch["category"] != before["category"] {
					return ErrInvalidAssistProposal
				}
			} else {
				name := strings.ToLower(strings.TrimSpace(heroString(patch["name"])))
				if newNames[name] {
					return ErrInvalidAssistProposal
				}
				newNames[name] = true
				fresh, err := id.New()
				if err != nil {
					return err
				}
				entryID = fresh.String()
				for _, row := range journal {
					if strings.EqualFold(heroString(row["name"]), heroString(patch["name"])) {
						return ErrInvalidAssistProposal
					}
				}
			}
			after = mergeHeroPatch(defaults, patch)
			after["entryId"] = entryID
			if strings.TrimSpace(heroString(after["name"])) == "" {
				return ErrInvalidAssistProposal
			}
			for _, key := range []string{"name", "description", "level", "evidence"} {
				if _, ok := after[key].(string); !ok {
					return ErrInvalidAssistProposal
				}
			}
			category := heroString(after["category"])
			if category != "ability" && category != "attribute" && category != "item" && category != "currency" {
				return ErrInvalidAssistProposal
			}
			status := heroString(after["status"])
			if status != "active" && status != "inactive" {
				return ErrInvalidAssistProposal
			}
			// Marshal through the exact command shape to reject fractional quantities
			// and non-string tags, including values merged from existing state.
			raw, _ := json.Marshal(after)
			var typed struct {
				Quantity int
				Tags     []string
			}
			if json.Unmarshal(raw, &typed) != nil || typed.Quantity < 0 {
				return ErrInvalidAssistProposal
			}
			if category == "item" || category == "currency" {
				limit := 9999
				if category == "currency" {
					limit = 999999999
				}
				if typed.Quantity > limit || (status == "inactive" && typed.Quantity != 0) || (category == "item" && status == "active" && typed.Quantity < 1) {
					return ErrInvalidAssistProposal
				}
			}
			target = "journal:" + entryID
		default:
			return ErrInvalidAssistProposal
		}
		if seen[target] {
			return ErrInvalidAssistProposal
		}
		seen[target] = true
		op.Before, _ = json.Marshal(before)
		op.Payload, _ = json.Marshal(after)
	}
	return nil
}

func heroString(value any) string {
	if value == nil {
		return ""
	}
	text, _ := value.(string)
	return text
}
func mergeHeroPatch(base, patch map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range base {
		result[key] = value
	}
	for key, value := range patch {
		result[key] = value
	}
	return result
}
func onlyHeroKeys(patch map[string]any, allowed ...string) bool {
	keys := map[string]bool{}
	for _, key := range allowed {
		keys[key] = true
	}
	for key, value := range patch {
		if !keys[key] || value == nil {
			return false
		}
	}
	return true
}
