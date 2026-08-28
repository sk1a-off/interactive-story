package director

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	domaindir "github.com/local/interactive-ai-story/backend/internal/domain/director"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/savepoint"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/directorrepo"
)

var ErrInvalidAssistProposal = errors.New("director assistant returned an invalid proposal")

type LLMFactory func(context.Context) (aiport.StoryLLM, error)

type SaveCreator interface {
	Create(context.Context, SaveCommand) (SaveResult, error)
}
type SaveCommand struct {
	TimelineID timeline.ID
	Name, Note string
	Kind       savepoint.Kind
	Pinned     bool
}
type SaveResult struct{ ID id.ID }

type Service struct {
	Repo       directorrepo.Repository
	Safety     func(context.Context, timeline.ID, string) (id.ID, error)
	LLM        aiport.StoryLLM
	LLMFactory LLMFactory
}

type AssistCommand struct {
	TimelineID  timeline.ID `json:"timelineId"`
	Instruction string      `json:"instruction"`
	Section     string      `json:"section"`
	SelectedID  string      `json:"selectedId,omitempty"`
}

type AssistOperation struct {
	Type            domaindir.ExactCommandType `json:"type"`
	Reference       string                     `json:"reference,omitempty"`
	ParentReference string                     `json:"parentReference,omitempty"`
	Payload         json.RawMessage            `json:"payload"`
	Note            string                     `json:"note,omitempty"`
}

type AssistResult struct {
	GenerationID id.ID             `json:"generationId"`
	Summary      string            `json:"summary"`
	Operations   []AssistOperation `json:"operations"`
}

func (s Service) View(ctx context.Context, tid timeline.ID) (directorrepo.View, error) {
	return s.Repo.View(ctx, tid)
}
func (s Service) ApplyExact(ctx context.Context, c domaindir.ExactCommand) (domaindir.AuditEntry, error) {
	if e := c.Validate(); e != nil {
		return domaindir.AuditEntry{}, e
	}
	if s.Safety != nil {
		sid, e := s.Safety(ctx, c.TimelineID, "Director safety: "+string(c.Type))
		if e != nil {
			return domaindir.AuditEntry{}, fmt.Errorf("create director safety save: %w", e)
		}
		c.Note = fmt.Sprintf("%s [safety_save=%s]", c.Note, sid)
	}
	return s.Repo.ApplyExact(ctx, c)
}
func (s Service) AddInstruction(ctx context.Context, c domaindir.InstructionCommand) (narrative.DirectorInstruction, error) {
	return s.Repo.AddInstruction(ctx, c)
}
func (s Service) History(ctx context.Context, tid timeline.ID, limit int) ([]domaindir.AuditEntry, error) {
	return s.Repo.ListAudit(ctx, tid, limit)
}

func (s Service) Assist(ctx context.Context, c AssistCommand) (AssistResult, error) {
	c.Instruction = strings.TrimSpace(c.Instruction)
	c.Section = strings.TrimSpace(c.Section)
	if c.TimelineID == "" || c.Instruction == "" || len([]rune(c.Instruction)) > 4000 || !validAssistSection(c.Section) {
		return AssistResult{}, domaindir.ErrInvalidCommand
	}
	view, err := s.Repo.View(ctx, c.TimelineID)
	if err != nil {
		return AssistResult{}, err
	}
	llm := s.LLM
	if s.LLMFactory != nil {
		llm, err = s.LLMFactory(ctx)
		if err != nil {
			return AssistResult{}, err
		}
	}
	if llm == nil {
		return AssistResult{}, errors.New("director assistant provider unavailable")
	}
	input := map[string]any{"instruction": c.Instruction, "section": c.Section, "selectedId": c.SelectedID, "characters": view.Characters, "locations": view.Locations, "objectives": view.Objectives, "relationships": view.Relationships, "worldCanon": map[string]any{"systems": view.WorldSystems, "rules": view.WorldRules, "resources": view.WorldResources}, "heroJournal": map[string]any{"abilities": view.Abilities, "attributes": view.Attributes, "inventory": view.Inventory}}
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		if last != nil {
			input["correction"] = "Previous output violated the operation contract. Return fewer operations with valid payloads and exact existing IDs."
		}
		raw, _ := json.Marshal(input)
		response, e := llm.Generate(ctx, aiport.StoryRequest{Role: "director_editor", PromptVersion: "v1", Input: raw, MaxTokens: 3000})
		if e != nil {
			last = e
			continue
		}
		var result AssistResult
		if json.Unmarshal(response.Output, &result) != nil {
			last = ErrInvalidAssistProposal
			continue
		}
		if result.Operations == nil || len(result.Operations) > 6 {
			last = ErrInvalidAssistProposal
			continue
		}
		if e = normalizeAssistOperations(&result, c.Section, view); e != nil {
			last = e
			continue
		}
		gid, e := id.New()
		if e != nil {
			return AssistResult{}, e
		}
		result.GenerationID = gid
		result.Summary = strings.TrimSpace(result.Summary)
		if result.Summary == "" {
			result.Summary = "Предложены адресные изменения живого мира."
		}
		return result, nil
	}
	if last == nil {
		last = ErrInvalidAssistProposal
	}
	return AssistResult{}, last
}

func validAssistSection(v string) bool {
	switch v {
	case "overview", "characters", "locations", "objectives", "world_rules":
		return true
	}
	return false
}

func normalizeAssistOperations(result *AssistResult, section string, view directorrepo.View) error {
	refs := map[string]string{}
	for i := range result.Operations {
		op := &result.Operations[i]
		if !operationMatchesSection(op.Type, section) || !json.Valid(op.Payload) {
			return ErrInvalidAssistProposal
		}
		var payload map[string]any
		if json.Unmarshal(op.Payload, &payload) != nil {
			return ErrInvalidAssistProposal
		}
		idField := ""
		switch op.Type {
		case domaindir.UpsertCharacter:
			idField = "characterId"
		case domaindir.UpsertLocation:
			idField = "locationId"
		case domaindir.UpsertObjective:
			idField = "objectiveId"
		case domaindir.UpsertWorldSystem:
			idField = "systemId"
		case domaindir.UpsertWorldRule:
			idField = "ruleId"
		}
		if idField != "" {
			value, exists := payload[idField]
			entityID := strings.TrimSpace(fmt.Sprint(value))
			if !exists || value == nil || entityID == "" || entityID == "<nil>" {
				fresh, e := id.New()
				if e != nil {
					return e
				}
				entityID = fresh.String()
				payload[idField] = entityID
			}
			if op.Reference != "" {
				refs[op.Reference] = entityID
			}
		}
		op.Payload, _ = json.Marshal(payload)
	}
	for i := range result.Operations {
		op := &result.Operations[i]
		var payload map[string]any
		_ = json.Unmarshal(op.Payload, &payload)
		if op.ParentReference != "" {
			parent, ok := refs[op.ParentReference]
			if !ok {
				return ErrInvalidAssistProposal
			}
			payload["parentObjectiveId"] = parent
			op.Payload, _ = json.Marshal(payload)
		}
		if err := validateAssistPayload(*op, view); err != nil {
			return err
		}
	}
	return nil
}

func operationMatchesSection(t domaindir.ExactCommandType, section string) bool {
	if section == "overview" {
		return true
	}
	switch section {
	case "characters":
		return t == domaindir.UpsertCharacter || t == domaindir.ArchiveCharacter
	case "locations":
		return t == domaindir.UpsertLocation || t == domaindir.ArchiveLocation
	case "objectives":
		return t == domaindir.UpsertObjective || t == domaindir.ArchiveObjective
	case "world_rules":
		return t == domaindir.UpsertWorldSystem || t == domaindir.UpsertWorldRule || t == domaindir.ArchiveWorldRule || t == domaindir.UpdateWorldResource
	}
	return false
}

func validateAssistPayload(op AssistOperation, view directorrepo.View) error {
	var p map[string]any
	if json.Unmarshal(op.Payload, &p) != nil {
		return ErrInvalidAssistProposal
	}
	required := func(key string) bool {
		return strings.TrimSpace(fmt.Sprint(p[key])) != "" && fmt.Sprint(p[key]) != "<nil>"
	}
	switch op.Type {
	case domaindir.UpsertCharacter:
		age, ok := p["age"].(float64)
		if !required("characterId") || !required("name") || !ok || age < 1 || age > 150 {
			return ErrInvalidAssistProposal
		}
	case domaindir.ArchiveCharacter:
		if !required("characterId") {
			return ErrInvalidAssistProposal
		}
		found := false
		for _, c := range view.Characters {
			if fmt.Sprint(c["id"]) == fmt.Sprint(p["characterId"]) {
				found = true
				if fmt.Sprint(c["kind"]) == "player" {
					return ErrInvalidAssistProposal
				}
			}
		}
		if !found {
			return ErrInvalidAssistProposal
		}
	case domaindir.UpsertLocation:
		if !required("locationId") || !required("name") {
			return ErrInvalidAssistProposal
		}
	case domaindir.ArchiveLocation:
		if !required("locationId") {
			return ErrInvalidAssistProposal
		}
		found := false
		for _, location := range view.Locations {
			if fmt.Sprint(location["id"]) == fmt.Sprint(p["locationId"]) {
				found = true
				break
			}
		}
		if !found {
			return ErrInvalidAssistProposal
		}
	case domaindir.UpsertObjective:
		if !required("objectiveId") || !required("title") || (fmt.Sprint(p["scope"]) != "global" && fmt.Sprint(p["scope"]) != "minor") {
			return ErrInvalidAssistProposal
		}
	case domaindir.ArchiveObjective:
		if !required("objectiveId") {
			return ErrInvalidAssistProposal
		}
		found := false
		for _, objective := range view.Objectives {
			if fmt.Sprint(objective["id"]) == fmt.Sprint(p["objectiveId"]) {
				found = true
				break
			}
		}
		if !found {
			return ErrInvalidAssistProposal
		}
	case domaindir.UpsertWorldSystem:
		if !required("systemId") || !required("name") {
			return ErrInvalidAssistProposal
		}
	case domaindir.UpsertWorldRule:
		if !required("ruleId") || !required("systemId") || !required("title") || !required("statement") {
			return ErrInvalidAssistProposal
		}
	case domaindir.ArchiveWorldRule:
		if !required("ruleId") {
			return ErrInvalidAssistProposal
		}
		found := false
		for _, rule := range view.WorldRules {
			if fmt.Sprint(rule["ruleId"]) == fmt.Sprint(p["ruleId"]) {
				found = true
				break
			}
		}
		if !found {
			return ErrInvalidAssistProposal
		}
	case domaindir.UpdateWorldResource:
		value, ok := p["currentValue"].(float64)
		if !required("resourceId") || !required("systemId") || !required("name") || !ok || value != value {
			return ErrInvalidAssistProposal
		}
	default:
		return ErrInvalidAssistProposal
	}
	return nil
}
