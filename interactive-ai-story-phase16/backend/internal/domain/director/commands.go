package director

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

var (
	ErrInvalidCommand = errors.New("invalid director command")
	ErrRevisionMoved  = errors.New("timeline semantic revision moved")
)

type ExactCommandType string

const (
	SetStat              ExactCommandType = "set_stat"
	TransferItem         ExactCommandType = "transfer_item"
	UpsertFact           ExactCommandType = "upsert_fact"
	GrantKnowledge       ExactCommandType = "grant_knowledge"
	UpsertBelief         ExactCommandType = "upsert_belief"
	UpdateCharacterState ExactCommandType = "update_character_state"
	UpdateRelationship   ExactCommandType = "update_relationship"
	UpdateThread         ExactCommandType = "update_thread"
	UpsertCharacter      ExactCommandType = "upsert_character"
	ArchiveCharacter     ExactCommandType = "archive_character"
	UpsertLocation       ExactCommandType = "upsert_location"
	ArchiveLocation      ExactCommandType = "archive_location"
	UpsertObjective      ExactCommandType = "upsert_objective"
	ArchiveObjective     ExactCommandType = "archive_objective"
	UpsertJournalEntry   ExactCommandType = "upsert_journal_entry"
	UpsertWorldSystem    ExactCommandType = "upsert_world_system"
	UpsertWorldRule      ExactCommandType = "upsert_world_rule"
	ArchiveWorldRule     ExactCommandType = "archive_world_rule"
	UpdateWorldResource  ExactCommandType = "update_world_resource"
	UpdateInstruction    ExactCommandType = "update_instruction"
)

type ExactCommand struct {
	TimelineID       timeline.ID      `json:"timelineId"`
	ExpectedRevision int64            `json:"expectedRevision"`
	Type             ExactCommandType `json:"type"`
	TargetID         id.ID            `json:"targetId"`
	Payload          json.RawMessage  `json:"payload"`
	Note             string           `json:"note,omitempty"`
}

func (c ExactCommand) Validate() error {
	if id.ID(c.TimelineID).IsZero() || c.ExpectedRevision < 1 || c.Type == "" || !json.Valid(c.Payload) {
		return ErrInvalidCommand
	}
	switch c.Type {
	case SetStat, TransferItem, UpsertFact, GrantKnowledge, UpsertBelief, UpdateCharacterState, UpdateRelationship, UpdateThread,
		UpsertCharacter, ArchiveCharacter, UpsertLocation, ArchiveLocation, UpsertObjective, ArchiveObjective, UpsertJournalEntry, UpdateInstruction:
		return nil
	case UpsertWorldSystem, UpsertWorldRule, ArchiveWorldRule, UpdateWorldResource:
		return nil
	default:
		return ErrInvalidCommand
	}
}

type InstructionCommand struct {
	TimelineID timeline.ID                `json:"timelineId"`
	Text       string                     `json:"text"`
	Scope      narrative.DirectorScope    `json:"scope"`
	Priority   narrative.DirectorPriority `json:"priority"`
}

func (c InstructionCommand) Validate() error {
	if id.ID(c.TimelineID).IsZero() || strings.TrimSpace(c.Text) == "" {
		return ErrInvalidCommand
	}
	switch c.Scope {
	case narrative.ScopeNextBeat, narrative.ScopeScene, narrative.ScopeChapter, narrative.ScopeTemporary, narrative.ScopePersistent:
	default:
		return ErrInvalidCommand
	}
	switch c.Priority {
	case narrative.PriorityLow, narrative.PriorityNormal, narrative.PriorityHigh, narrative.PriorityHard:
	default:
		return ErrInvalidCommand
	}
	return nil
}

type AuditEntry struct {
	ID          id.ID           `json:"id"`
	TimelineID  timeline.ID     `json:"timelineId"`
	CommandType string          `json:"commandType"`
	TargetType  string          `json:"targetType"`
	TargetID    id.ID           `json:"targetId,omitempty"`
	Before      json.RawMessage `json:"before,omitempty"`
	After       json.RawMessage `json:"after,omitempty"`
	Note        string          `json:"note,omitempty"`
}
