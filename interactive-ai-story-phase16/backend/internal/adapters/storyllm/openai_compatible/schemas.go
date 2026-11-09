package openai_compatible

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// ResponseSchemaFingerprint identifies the exact shapes used by benchmarks.
func ResponseSchemaFingerprint() string {
	schemas := map[string]any{}
	for _, role := range []string{"action_interpreter", "director", "pacing", "turn_planner", "writer", "choices", "world_evaluator", "state_evaluator", "quest_evaluator"} {
		schemas[role] = roleSchema(role)
	}
	raw, _ := json.Marshal(schemas)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Schemas constrain wire shape only. Evidence, agency, UUID visibility and Canon
// invariants remain application validations. Flexible/editor/setup roles retain
// json_object until their operation-specific unions have measured contracts.
func roleSchema(role string) map[string]any {
	str := func() map[string]any { return map[string]any{"type": "string"} }
	object := func(props map[string]any, required ...string) map[string]any {
		if required == nil {
			required = []string{}
		}
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	}
	stringsObject := func(names ...string) map[string]any {
		p := map[string]any{}
		for _, n := range names {
			p[n] = str()
		}
		return p
	}
	array := func(item map[string]any) map[string]any { return map[string]any{"type": "array", "items": item} }
	transition := str()
	transition["enum"] = []string{"continue_scene", "new_scene", "new_chapter"}
	switch role {
	case "action_interpreter":
		return object(stringsObject("intent"), "intent")
	case "director":
		return object(stringsObject("goal"), "goal")
	case "writer":
		return object(stringsObject("text"), "text")
	case "pacing":
		p := stringsObject("chapterTitle", "chapterGoal", "chapterTone", "sceneGoal", "sceneMood", "locationId")
		p["transition"] = transition
		return object(p, "transition")
	case "turn_planner":
		p := stringsObject("intent", "immediateGoal")
		p["transition"] = transition
		p["scene"] = object(stringsObject("goal", "mood", "locationId"))
		p["chapter"] = object(stringsObject("title", "goal", "tone"))
		p["riskFlags"] = array(str())
		return object(p, "intent", "immediateGoal", "transition")
	case "choices":
		a := array(object(stringsObject("id", "label"), "id", "label"))
		a["minItems"], a["maxItems"] = 4, 4
		return object(map[string]any{"choices": a}, "choices")
	case "world_evaluator":
		p := stringsObject("type", "id", "characterId", "locationId", "name", "role", "personality", "relationship", "visualAnchorEn", "mood", "currentGoal", "description", "evidenceQuote")
		p["age"] = map[string]any{"type": "integer"}
		return object(map[string]any{"changes": array(object(p, "type", "evidenceQuote"))}, "changes")
	case "state_evaluator":
		p := stringsObject("operation", "entryId", "category", "name", "description", "level", "status", "evidence", "evidenceQuote")
		p["quantity"] = map[string]any{"type": "integer", "minimum": 0}
		p["tags"] = array(str())
		return object(map[string]any{"changes": array(object(p, "operation", "evidenceQuote"))}, "changes")
	case "quest_evaluator":
		p := stringsObject("operation", "objectiveId", "reference", "parentObjectiveId", "parentReference", "scope", "kind", "questType", "title", "description", "successCriteria", "status", "evidence", "evidenceQuote")
		p["progress"] = map[string]any{"type": "integer", "minimum": 0, "maximum": 100}
		return object(map[string]any{"changes": array(object(p, "operation", "evidenceQuote"))}, "changes")
	default:
		return nil
	}
}
