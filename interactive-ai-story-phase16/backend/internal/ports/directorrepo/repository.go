package directorrepo

import (
	"context"
	"encoding/json"
	"github.com/local/interactive-ai-story/backend/internal/domain/director"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type View struct {
	TimelineID       timeline.ID       `json:"timelineId"`
	SemanticRevision int64             `json:"semanticRevision"`
	HeadEventSeq     int64             `json:"headEventSeq"`
	Characters       []map[string]any  `json:"characters"`
	Locations        []map[string]any  `json:"locations"`
	Relationships    []map[string]any  `json:"relationships"`
	Stats            []map[string]any  `json:"stats"`
	Items            []map[string]any  `json:"items"`
	Facts            []map[string]any  `json:"facts"`
	Knowledge        []map[string]any  `json:"knowledge"`
	Beliefs          []map[string]any  `json:"beliefs"`
	Threads          []map[string]any  `json:"threads"`
	Objectives       []map[string]any  `json:"objectives"`
	Abilities        []map[string]any  `json:"abilities"`
	Attributes       []map[string]any  `json:"attributes"`
	Inventory        []map[string]any  `json:"inventory"`
	WorldSystems     []map[string]any  `json:"worldSystems"`
	WorldRules       []map[string]any  `json:"worldRules"`
	WorldResources   []map[string]any  `json:"worldResources"`
	WorldRuleAudit   []map[string]any  `json:"worldRuleAudit"`
	Instructions     []InstructionView `json:"instructions"`
}

// InstructionView is the public Director API shape. It is intentionally
// separate from narrative.DirectorInstruction because that domain type is
// part of the hashed snapshot serialization contract.
type InstructionView struct {
	ID           id.ID                      `json:"id"`
	TimelineID   id.ID                      `json:"timelineId"`
	Text         string                     `json:"text"`
	Scope        narrative.DirectorScope    `json:"scope"`
	Priority     narrative.DirectorPriority `json:"priority"`
	Status       string                     `json:"status"`
	CreatedAtSeq int64                      `json:"createdAtSeq"`
	ExpiresAt    json.RawMessage            `json:"expiresAt,omitempty"`
}
type Repository interface {
	View(context.Context, timeline.ID) (View, error)
	ApplyExact(context.Context, director.ExactCommand) (director.AuditEntry, error)
	AddInstruction(context.Context, director.InstructionCommand) (narrative.DirectorInstruction, error)
	ListAudit(context.Context, timeline.ID, int) ([]director.AuditEntry, error)
	ActiveInstructions(context.Context, timeline.ID) ([]narrative.DirectorInstruction, error)
}
type SafetySaver interface {
	CreateSafetySave(context.Context, timeline.ID, string) (id.ID, error)
}
type JSON = json.RawMessage
