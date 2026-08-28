package projection

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
)

var ErrUnsupportedSemanticEvent = errors.New("unsupported semantic event")

type chapterCreated struct {
	ID     id.ID  `json:"chapterId"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Goal   string `json:"goal"`
	Tone   string `json:"tone"`
	Status string `json:"status"`
}
type sceneStarted struct {
	ID         id.ID  `json:"sceneId"`
	ChapterID  id.ID  `json:"chapterId"`
	LocationID id.ID  `json:"locationId"`
	Number     int    `json:"number"`
	Mood       string `json:"mood"`
	Goal       string `json:"goal"`
	Status     string `json:"status"`
}
type lifecycleCompleted struct {
	ChapterID id.ID  `json:"chapterId"`
	SceneID   id.ID  `json:"sceneId"`
	Status    string `json:"status"`
}
type beatCommitted struct {
	BeatID   id.ID              `json:"beatId"`
	SceneID  id.ID              `json:"sceneId"`
	Position int                `json:"position"`
	Kind     narrative.BeatKind `json:"kind"`
	Text     string             `json:"text"`
	Status   string             `json:"status"`
}
type directorExact struct {
	CommandType string          `json:"commandType"`
	TargetType  string          `json:"targetType"`
	TargetID    id.ID           `json:"targetId"`
	After       json.RawMessage `json:"after"`
}
type objectiveEvent struct {
	ID                id.ID                     `json:"objectiveId"`
	ParentObjectiveID id.ID                     `json:"parentObjectiveId"`
	Scope             narrative.ObjectiveScope  `json:"scope"`
	Kind              narrative.ObjectiveKind   `json:"kind"`
	QuestType         narrative.QuestType       `json:"questType"`
	Title             string                    `json:"title"`
	Description       string                    `json:"description"`
	SuccessCriteria   string                    `json:"successCriteria"`
	Status            narrative.ObjectiveStatus `json:"status"`
	Progress          int                       `json:"progress"`
	Evidence          string                    `json:"evidence"`
}
type journalEntryEvent struct {
	ID          id.ID    `json:"entryId"`
	Category    string   `json:"category"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Quantity    int      `json:"quantity"`
	Level       string   `json:"level"`
	Status      string   `json:"status"`
	Evidence    string   `json:"evidence"`
	Tags        []string `json:"tags"`
}
type worldCharacterEvent struct {
	CharacterID                   id.ID `json:"characterId"`
	Name, Kind, Role, Personality string
	Relationship, VisualAnchorEn  string
	Mood, CurrentGoal             string
	Age                           int
	Active                        bool
}
type worldLocationEvent struct {
	LocationID                        id.ID `json:"locationId"`
	Name, Description, VisualAnchorEn string
	Active                            bool
}
type instructionsExpired struct {
	InstructionIDs []id.ID `json:"instructionIds"`
}
type instructionAdded struct {
	ID           id.ID                      `json:"instructionId"`
	TimelineID   id.ID                      `json:"timelineId"`
	Text         string                     `json:"text"`
	Scope        narrative.DirectorScope    `json:"scope"`
	Priority     narrative.DirectorPriority `json:"priority"`
	Status       string                     `json:"status"`
	CreatedAtSeq int64                      `json:"createdAtSeq"`
}
type worldSystemEvent struct {
	ID          string          `json:"systemId"`
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Description string          `json:"description"`
	Resources   json.RawMessage `json:"resources"`
	Status      string          `json:"status"`
	Version     int64           `json:"version"`
}
type worldRuleEvent struct {
	ID               string          `json:"ruleId"`
	SystemID         string          `json:"systemId"`
	Title            string          `json:"title"`
	Category         string          `json:"category"`
	Severity         string          `json:"severity"`
	Statement        string          `json:"statement"`
	Preconditions    json.RawMessage `json:"preconditions"`
	Costs            json.RawMessage `json:"costs"`
	ForbiddenResults json.RawMessage `json:"forbiddenResults"`
	Exceptions       json.RawMessage `json:"exceptions"`
	Tags             json.RawMessage `json:"tags"`
	Visibility       string          `json:"visibility"`
	Status           string          `json:"status"`
	ExceptionOf      string          `json:"exceptionOf"`
	Source           string          `json:"source"`
	Evidence         string          `json:"evidence"`
	Version          int64           `json:"version"`
}
type worldResourceEvent struct {
	ResourceID string   `json:"resourceId"`
	SystemID   string   `json:"systemId"`
	OwnerType  string   `json:"ownerType"`
	OwnerID    id.ID    `json:"ownerId"`
	OwnerKey   string   `json:"ownerKey"`
	Name       string   `json:"name"`
	Unit       string   `json:"unit"`
	Current    float64  `json:"currentValue"`
	Minimum    *float64 `json:"minValue"`
	Maximum    *float64 `json:"maxValue"`
	Visibility string   `json:"visibility"`
	Version    int64    `json:"version"`
}

func applySemantic(state *TimelineState, e event.StoredEvent) error {
	switch e.Type {
	case "player_action_attempted", "choices_ready":
		return nil
	case "chapter_created":
		var p chapterCreated
		if json.Unmarshal(e.Payload, &p) != nil || p.ID.IsZero() {
			return ErrUnsupportedSemanticEvent
		}
		state.Narrative.Chapters[p.ID] = narrative.Chapter{ID: p.ID, TimelineID: id.ID(state.TimelineID), Number: p.Number, Title: p.Title, Goal: p.Goal, Tone: p.Tone, Status: p.Status}
		return nil
	case "scene_started":
		var p sceneStarted
		if json.Unmarshal(e.Payload, &p) != nil || p.ID.IsZero() || p.ChapterID.IsZero() {
			return ErrUnsupportedSemanticEvent
		}
		state.Narrative.Scenes[p.ID] = narrative.Scene{ID: p.ID, ChapterID: p.ChapterID, LocationID: p.LocationID, Number: p.Number, Mood: p.Mood, Goal: p.Goal, Status: p.Status}
		return nil
	case "scene_completed":
		var p lifecycleCompleted
		if json.Unmarshal(e.Payload, &p) != nil || p.SceneID.IsZero() || p.Status != "completed" {
			return ErrUnsupportedSemanticEvent
		}
		current, ok := state.Narrative.Scenes[p.SceneID]
		if !ok {
			return narrative.ErrNotFound
		}
		current.Status = p.Status
		state.Narrative.Scenes[p.SceneID] = current
		return nil
	case "chapter_completed":
		var p lifecycleCompleted
		if json.Unmarshal(e.Payload, &p) != nil || p.ChapterID.IsZero() || p.Status != "completed" {
			return ErrUnsupportedSemanticEvent
		}
		current, ok := state.Narrative.Chapters[p.ChapterID]
		if !ok {
			return narrative.ErrNotFound
		}
		current.Status = p.Status
		state.Narrative.Chapters[p.ChapterID] = current
		return nil
	case "beat_committed":
		var p beatCommitted
		if json.Unmarshal(e.Payload, &p) != nil || p.BeatID.IsZero() || p.SceneID.IsZero() || p.Position < 1 {
			return ErrUnsupportedSemanticEvent
		}
		for k, v := range state.Narrative.Beats {
			if v.SceneID == p.SceneID && v.Position == p.Position && v.IsActive {
				v.IsActive = false
				state.Narrative.Beats[k] = v
			}
		}
		content, _ := json.Marshal(map[string]any{"text": p.Text})
		state.Narrative.Beats[p.BeatID] = narrative.Beat{ID: p.BeatID, SceneID: p.SceneID, Position: p.Position, Kind: p.Kind, Content: content, Status: p.Status, IsActive: true, GenerationID: generationID(e)}
		return nil
	case "character_world_upserted":
		var p worldCharacterEvent
		if json.Unmarshal(e.Payload, &p) != nil || p.CharacterID.IsZero() || strings.TrimSpace(p.Name) == "" || p.Age < 1 || p.Age > 150 || p.Kind != "persistent_npc" {
			return ErrUnsupportedSemanticEvent
		}
		profile, _ := json.Marshal(map[string]any{"name": p.Name, "age": p.Age, "kind": p.Kind, "role": p.Role, "personality": p.Personality, "relationship": p.Relationship, "visualAnchorEn": p.VisualAnchorEn})
		current := state.Narrative.Characters[p.CharacterID]
		current.TimelineID, current.CharacterID = id.ID(state.TimelineID), p.CharacterID
		current.Mood, current.CurrentGoal, current.Active, current.Profile = p.Mood, p.CurrentGoal, p.Active, profile
		current.Version++
		state.Narrative.Characters[p.CharacterID] = current
		return nil
	case "location_world_upserted":
		var p worldLocationEvent
		if json.Unmarshal(e.Payload, &p) != nil || p.LocationID.IsZero() || strings.TrimSpace(p.Name) == "" {
			return ErrUnsupportedSemanticEvent
		}
		visual, _ := json.Marshal(map[string]any{"anchorEn": p.VisualAnchorEn})
		current := state.Narrative.Locations[p.LocationID]
		current.TimelineID, current.LocationID = id.ID(state.TimelineID), p.LocationID
		current.Name, current.Description, current.VisualProfile, current.Active = p.Name, p.Description, visual, p.Active
		current.Version++
		state.Narrative.Locations[p.LocationID] = current
		return nil
	case "objective_created", "objective_updated":
		var p objectiveEvent
		if json.Unmarshal(e.Payload, &p) != nil || p.ID.IsZero() || p.Title == "" || (p.Scope != narrative.ObjectiveGlobal && p.Scope != narrative.ObjectiveMinor) || p.Progress < 0 || p.Progress > 100 {
			return ErrUnsupportedSemanticEvent
		}
		if p.Status != narrative.ObjectiveActive && p.Status != narrative.ObjectiveCompleted && p.Status != narrative.ObjectiveFailed {
			return ErrUnsupportedSemanticEvent
		}
		if p.Kind == "" {
			if p.Scope == narrative.ObjectiveGlobal {
				p.Kind = narrative.ObjectiveQuest
			} else {
				p.Kind = narrative.ObjectiveTask
			}
		}
		if p.Scope == narrative.ObjectiveGlobal && (p.Kind != narrative.ObjectiveQuest || !p.ParentObjectiveID.IsZero()) {
			return ErrUnsupportedSemanticEvent
		}
		if p.QuestType == "" {
			p.QuestType = narrative.QuestMain
		}
		if p.QuestType != narrative.QuestMain && p.QuestType != narrative.QuestSide {
			return ErrUnsupportedSemanticEvent
		}
		if p.Scope == narrative.ObjectiveMinor && p.Kind != narrative.ObjectiveTask && p.Kind != narrative.ObjectiveEvent && p.Kind != narrative.ObjectiveMilestone {
			return ErrUnsupportedSemanticEvent
		}
		introduced := e.Seq
		if current, ok := state.Narrative.Threads[p.ID]; ok {
			introduced = current.IntroducedAtSeq
		} else if e.Type == "objective_updated" {
			return narrative.ErrNotFound
		}
		status := narrative.ThreadOpen
		if p.Status == narrative.ObjectiveCompleted {
			status = narrative.ThreadClosed
		} else if p.Status == narrative.ObjectiveFailed {
			status = narrative.ThreadAbandoned
		}
		metadata, _ := json.Marshal(map[string]any{"kind": "objective", "scope": p.Scope, "objectiveKind": p.Kind, "questType": p.QuestType, "parentObjectiveId": p.ParentObjectiveID, "description": p.Description, "successCriteria": p.SuccessCriteria, "progress": p.Progress, "evidence": p.Evidence})
		importance := float64(.6)
		if p.Scope == narrative.ObjectiveGlobal {
			importance = 1
		}
		state.Narrative.Threads[p.ID] = narrative.StoryThread{ID: p.ID, TimelineID: id.ID(state.TimelineID), Title: p.Title, Summary: p.Description, Importance: importance, Status: status, Metadata: metadata, IntroducedAtSeq: introduced, LastTouchedAtSeq: e.Seq}
		return nil
	case "journal_entry_created", "journal_entry_updated":
		var p journalEntryEvent
		if json.Unmarshal(e.Payload, &p) != nil || p.ID.IsZero() || strings.TrimSpace(p.Name) == "" || (p.Category != "ability" && p.Category != "attribute" && p.Category != "item" && p.Category != "currency") || (p.Status != "active" && p.Status != "inactive") || p.Quantity < 0 {
			return ErrUnsupportedSemanticEvent
		}
		if p.Category == "item" && p.Status == "active" && p.Quantity < 1 {
			return ErrUnsupportedSemanticEvent
		}
		introduced := e.Seq
		if current, ok := state.Narrative.Threads[p.ID]; ok {
			introduced = current.IntroducedAtSeq
		} else if e.Type == "journal_entry_updated" {
			return narrative.ErrNotFound
		}
		threadStatus := narrative.ThreadOpen
		if p.Status == "inactive" {
			threadStatus = narrative.ThreadClosed
		}
		metadata, _ := json.Marshal(map[string]any{"kind": "hero_journal", "category": p.Category, "quantity": p.Quantity, "level": p.Level, "evidence": p.Evidence, "tags": p.Tags})
		importance := .65
		if p.Category == "ability" {
			importance = .75
		} else if p.Category == "attribute" {
			importance = .7
		} else if p.Category == "currency" {
			importance = .7
		}
		state.Narrative.Threads[p.ID] = narrative.StoryThread{ID: p.ID, TimelineID: id.ID(state.TimelineID), Title: strings.TrimSpace(p.Name), Summary: strings.TrimSpace(p.Description), Importance: importance, Status: threadStatus, Metadata: metadata, IntroducedAtSeq: introduced, LastTouchedAtSeq: e.Seq}
		return nil
	case "director_instructions_expired":
		var p instructionsExpired
		if json.Unmarshal(e.Payload, &p) != nil || len(p.InstructionIDs) == 0 {
			return ErrUnsupportedSemanticEvent
		}
		for _, instructionID := range p.InstructionIDs {
			current, ok := state.Narrative.DirectorInstructions[instructionID]
			if !ok {
				// Legacy instructions were stored outside the event stream. They can
				// legitimately be absent when replay starts from an older snapshot;
				// materialized narrative export restores their current final state.
				continue
			}
			current.Status = "expired"
			state.Narrative.DirectorInstructions[instructionID] = current
		}
		return nil
	case "director_instruction_added":
		var p instructionAdded
		if json.Unmarshal(e.Payload, &p) != nil || p.ID.IsZero() || strings.TrimSpace(p.Text) == "" || p.Status != "active" {
			return ErrUnsupportedSemanticEvent
		}
		state.Narrative.DirectorInstructions[p.ID] = narrative.DirectorInstruction{
			ID: p.ID, TimelineID: id.ID(state.TimelineID), Text: strings.TrimSpace(p.Text), Scope: p.Scope,
			Priority: p.Priority, Status: p.Status, CreatedAtSeq: p.CreatedAtSeq,
		}
		return nil
	case "director_exact_edit_applied":
		var p directorExact
		if json.Unmarshal(e.Payload, &p) != nil || !json.Valid(p.After) {
			return ErrUnsupportedSemanticEvent
		}
		return applyDirectorAfter(&state.Narrative, id.ID(state.TimelineID), e.Seq, p)
	case "world_system_upserted":
		var p worldSystemEvent
		if json.Unmarshal(e.Payload, &p) != nil || strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Name) == "" {
			return ErrUnsupportedSemanticEvent
		}
		if len(p.Resources) == 0 {
			p.Resources = json.RawMessage(`[]`)
		}
		if p.Status == "" {
			p.Status = "active"
		}
		if p.Version < 1 {
			p.Version = 1
		}
		return state.Narrative.PutWorldSystem(narrative.WorldSystem{TimelineID: id.ID(state.TimelineID), ID: p.ID, Name: p.Name, Kind: p.Kind, Description: p.Description, Resources: p.Resources, Status: p.Status, Version: p.Version})
	case "world_rule_upserted":
		var p worldRuleEvent
		if json.Unmarshal(e.Payload, &p) != nil || strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.SystemID) == "" || strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Statement) == "" {
			return ErrUnsupportedSemanticEvent
		}
		if p.Version < 1 {
			p.Version = 1
		}
		for _, raw := range []*json.RawMessage{&p.Preconditions, &p.Costs, &p.ForbiddenResults, &p.Exceptions, &p.Tags} {
			if len(*raw) == 0 {
				*raw = json.RawMessage(`[]`)
			}
		}
		return state.Narrative.PutWorldRule(narrative.WorldRule{TimelineID: id.ID(state.TimelineID), ID: p.ID, SystemID: p.SystemID, Title: p.Title, Category: p.Category, Severity: p.Severity, Statement: p.Statement, Preconditions: p.Preconditions, Costs: p.Costs, ForbiddenResults: p.ForbiddenResults, Exceptions: p.Exceptions, Tags: p.Tags, Visibility: p.Visibility, Status: p.Status, ExceptionOf: p.ExceptionOf, Source: p.Source, Evidence: p.Evidence, Version: p.Version, EstablishedAtSeq: e.Seq})
	case "world_resource_changed":
		var p worldResourceEvent
		if json.Unmarshal(e.Payload, &p) != nil || strings.TrimSpace(p.ResourceID) == "" || strings.TrimSpace(p.SystemID) == "" || strings.TrimSpace(p.Name) == "" {
			return ErrUnsupportedSemanticEvent
		}
		if p.Minimum != nil && p.Current < *p.Minimum || p.Maximum != nil && p.Current > *p.Maximum {
			return ErrUnsupportedSemanticEvent
		}
		if p.Version < 1 {
			p.Version = 1
		}
		return state.Narrative.PutWorldResource(narrative.WorldResourceState{TimelineID: id.ID(state.TimelineID), ResourceID: p.ResourceID, SystemID: p.SystemID, OwnerType: p.OwnerType, OwnerID: p.OwnerID, OwnerKey: p.OwnerKey, Name: p.Name, Unit: p.Unit, Current: p.Current, Minimum: p.Minimum, Maximum: p.Maximum, Visibility: p.Visibility, Version: p.Version})
	default:
		return ErrUnsupportedSemanticEvent
	}
}
func generationID(e event.StoredEvent) id.ID {
	if e.GenerationID == nil {
		return ""
	}
	return id.ID(*e.GenerationID)
}

func applyDirectorAfter(s *narrative.State, tid id.ID, seq int64, p directorExact) error {
	switch p.CommandType {
	case "set_stat":
		var v struct {
			OwnerType          narrative.StatOwnerType
			OwnerID            id.ID
			Key                string
			ValueType          narrative.ValueType
			Value, TargetValue json.RawMessage
			EvolutionMode      string
		}
		if json.Unmarshal(p.After, &v) != nil || v.OwnerID.IsZero() || v.Key == "" {
			return ErrUnsupportedSemanticEvent
		}
		return s.PutStat(narrative.Stat{TimelineID: tid, OwnerType: v.OwnerType, OwnerID: v.OwnerID, Key: v.Key, ValueType: v.ValueType, Value: v.Value, TargetValue: v.TargetValue, EvolutionMode: v.EvolutionMode})
	case "transfer_item":
		var v struct {
			ItemID, OwnerCharacterID, LocationID id.ID
			Condition                            string
		}
		if json.Unmarshal(p.After, &v) != nil || v.ItemID.IsZero() {
			return ErrUnsupportedSemanticEvent
		}
		current := s.Items[v.ItemID]
		current.TimelineID = tid
		current.ItemID = v.ItemID
		current.OwnerCharacterID = v.OwnerCharacterID
		current.LocationID = v.LocationID
		current.Condition = v.Condition
		current.Version++
		s.Items[v.ItemID] = current
		return nil
	case "upsert_fact":
		var v struct {
			ID          id.ID
			SubjectType string
			SubjectID   id.ID
			Predicate   string
			Object      json.RawMessage
		}
		if json.Unmarshal(p.After, &v) != nil || v.ID.IsZero() {
			return ErrUnsupportedSemanticEvent
		}
		s.Facts[v.ID] = narrative.Fact{ID: v.ID, TimelineID: tid, SubjectType: v.SubjectType, SubjectID: v.SubjectID, Predicate: v.Predicate, Object: v.Object, Status: narrative.FactActive, ValidFromSeq: seq}
		return nil
	case "grant_knowledge":
		var v struct {
			FactID, CharacterID id.ID
			Confidence          float64
		}
		if json.Unmarshal(p.After, &v) != nil {
			return ErrUnsupportedSemanticEvent
		}
		k, err := narrative.NewKnowledge(tid, v.FactID, v.CharacterID, v.Confidence, seq)
		if err != nil {
			return err
		}
		return s.GrantKnowledge(k)
	case "upsert_belief":
		var v struct {
			ID, CharacterID id.ID
			SubjectType     string
			SubjectID       id.ID
			Predicate       string
			Object          json.RawMessage
			Stance          narrative.BeliefStance
			Confidence      float64
		}
		if json.Unmarshal(p.After, &v) != nil || v.ID.IsZero() {
			return ErrUnsupportedSemanticEvent
		}
		b, err := narrative.NewBelief(tid, v.ID, v.CharacterID, v.Stance, v.Confidence)
		if err != nil {
			return err
		}
		b.SubjectType = v.SubjectType
		b.SubjectID = v.SubjectID
		b.Predicate = v.Predicate
		b.Object = v.Object
		ptr := seq
		b.SourceEventSeq = &ptr
		return s.PutBelief(b)
	case "update_character_state":
		var v struct {
			CharacterID       id.ID
			Mood, CurrentGoal string
			Active            bool
		}
		if json.Unmarshal(p.After, &v) != nil || v.CharacterID.IsZero() {
			return ErrUnsupportedSemanticEvent
		}
		cur := s.Characters[v.CharacterID]
		cur.TimelineID = tid
		cur.CharacterID = v.CharacterID
		cur.Mood = v.Mood
		cur.CurrentGoal = v.CurrentGoal
		cur.Active = v.Active
		cur.Version++
		s.Characters[v.CharacterID] = cur
		return nil
	case "upsert_character":
		var v struct {
			CharacterID                   id.ID
			Name, Kind, Role, Personality string
			Relationship, VisualAnchorEn  string
			Mood, CurrentGoal             string
			Age                           int
			Active                        bool
		}
		if json.Unmarshal(p.After, &v) != nil || v.CharacterID.IsZero() || strings.TrimSpace(v.Name) == "" {
			return ErrUnsupportedSemanticEvent
		}
		profile, _ := json.Marshal(map[string]any{"name": v.Name, "age": v.Age, "kind": v.Kind, "role": v.Role, "personality": v.Personality, "relationship": v.Relationship, "visualAnchorEn": v.VisualAnchorEn})
		cur := s.Characters[v.CharacterID]
		cur.TimelineID, cur.CharacterID = tid, v.CharacterID
		cur.Mood, cur.CurrentGoal, cur.Active, cur.Profile = v.Mood, v.CurrentGoal, v.Active, profile
		cur.Version++
		s.Characters[v.CharacterID] = cur
		return nil
	case "archive_character":
		var v struct{ CharacterID id.ID }
		if json.Unmarshal(p.After, &v) != nil || v.CharacterID.IsZero() {
			return ErrUnsupportedSemanticEvent
		}
		cur, ok := s.Characters[v.CharacterID]
		if !ok {
			return narrative.ErrNotFound
		}
		cur.Active = false
		cur.Version++
		s.Characters[v.CharacterID] = cur
		return nil
	case "upsert_location":
		var v struct {
			LocationID                        id.ID
			Name, Description, VisualAnchorEn string
			Active                            bool
		}
		if json.Unmarshal(p.After, &v) != nil || v.LocationID.IsZero() || strings.TrimSpace(v.Name) == "" {
			return ErrUnsupportedSemanticEvent
		}
		visual, _ := json.Marshal(map[string]any{"anchorEn": v.VisualAnchorEn})
		cur := s.Locations[v.LocationID]
		cur.TimelineID, cur.LocationID = tid, v.LocationID
		cur.Name, cur.Description, cur.VisualProfile, cur.Active = v.Name, v.Description, visual, v.Active
		cur.Version++
		s.Locations[v.LocationID] = cur
		return nil
	case "archive_location":
		var v struct{ LocationID id.ID }
		if json.Unmarshal(p.After, &v) != nil || v.LocationID.IsZero() {
			return ErrUnsupportedSemanticEvent
		}
		cur, ok := s.Locations[v.LocationID]
		if !ok {
			return narrative.ErrNotFound
		}
		cur.Active = false
		cur.Version++
		s.Locations[v.LocationID] = cur
		return nil
	case "upsert_objective":
		var v struct {
			ObjectiveID, ParentObjectiveID id.ID
			Scope                          narrative.ObjectiveScope
			Kind                           narrative.ObjectiveKind
			QuestType                      narrative.QuestType
			Title, Description             string
			SuccessCriteria, Evidence      string
			Status                         narrative.ObjectiveStatus
			Progress                       int
		}
		if json.Unmarshal(p.After, &v) != nil || v.ObjectiveID.IsZero() || strings.TrimSpace(v.Title) == "" {
			return ErrUnsupportedSemanticEvent
		}
		threadStatus := narrative.ThreadOpen
		if v.Status == narrative.ObjectiveCompleted {
			threadStatus = narrative.ThreadClosed
		}
		if v.Status == narrative.ObjectiveFailed {
			threadStatus = narrative.ThreadAbandoned
		}
		introduced := seq
		if old, ok := s.Threads[v.ObjectiveID]; ok {
			introduced = old.IntroducedAtSeq
		}
		metadata, _ := json.Marshal(map[string]any{"kind": "objective", "scope": v.Scope, "objectiveKind": v.Kind, "questType": v.QuestType, "parentObjectiveId": v.ParentObjectiveID, "description": v.Description, "successCriteria": v.SuccessCriteria, "progress": v.Progress, "evidence": v.Evidence, "source": "director"})
		importance := .6
		if v.Scope == narrative.ObjectiveGlobal {
			importance = 1
		}
		s.Threads[v.ObjectiveID] = narrative.StoryThread{ID: v.ObjectiveID, TimelineID: tid, Title: v.Title, Summary: v.Description, Importance: importance, Status: threadStatus, Metadata: metadata, IntroducedAtSeq: introduced, LastTouchedAtSeq: seq}
		return nil
	case "archive_objective":
		var v struct{ ObjectiveID id.ID }
		if json.Unmarshal(p.After, &v) != nil || v.ObjectiveID.IsZero() {
			return ErrUnsupportedSemanticEvent
		}
		cur, ok := s.Threads[v.ObjectiveID]
		if !ok {
			return narrative.ErrNotFound
		}
		cur.Status, cur.LastTouchedAtSeq = narrative.ThreadAbandoned, seq
		s.Threads[v.ObjectiveID] = cur
		for key, child := range s.Threads {
			var meta struct {
				ParentObjectiveID id.ID `json:"parentObjectiveId"`
			}
			_ = json.Unmarshal(child.Metadata, &meta)
			if meta.ParentObjectiveID == v.ObjectiveID {
				child.Status, child.LastTouchedAtSeq = narrative.ThreadAbandoned, seq
				s.Threads[key] = child
			}
		}
		return nil
	case "upsert_journal_entry":
		var v struct {
			EntryID                     id.ID
			Category, Name, Description string
			Level, Status, Evidence     string
			Quantity                    int
			Tags                        []string
		}
		if json.Unmarshal(p.After, &v) != nil || v.EntryID.IsZero() || strings.TrimSpace(v.Name) == "" || (v.Category != "ability" && v.Category != "attribute" && v.Category != "item" && v.Category != "currency") || (v.Status != "active" && v.Status != "inactive") || v.Quantity < 0 {
			return ErrUnsupportedSemanticEvent
		}
		threadStatus := narrative.ThreadOpen
		if v.Status == "inactive" {
			threadStatus = narrative.ThreadClosed
		}
		introduced := seq
		if current, ok := s.Threads[v.EntryID]; ok {
			introduced = current.IntroducedAtSeq
		}
		metadata, _ := json.Marshal(map[string]any{"kind": "hero_journal", "category": v.Category, "quantity": v.Quantity, "level": v.Level, "evidence": v.Evidence, "tags": v.Tags, "source": "director"})
		importance := .65
		if v.Category == "ability" {
			importance = .75
		} else if v.Category == "attribute" || v.Category == "currency" {
			importance = .7
		}
		s.Threads[v.EntryID] = narrative.StoryThread{ID: v.EntryID, TimelineID: tid, Title: strings.TrimSpace(v.Name), Summary: strings.TrimSpace(v.Description), Importance: importance, Status: threadStatus, Metadata: metadata, IntroducedAtSeq: introduced, LastTouchedAtSeq: seq}
		return nil
	case "upsert_world_system":
		var v struct {
			SystemID    string          `json:"systemId"`
			Name        string          `json:"name"`
			Kind        string          `json:"kind"`
			Description string          `json:"description"`
			Resources   json.RawMessage `json:"resources"`
			Status      string          `json:"status"`
			Version     int64           `json:"version"`
		}
		if json.Unmarshal(p.After, &v) != nil || strings.TrimSpace(v.SystemID) == "" || strings.TrimSpace(v.Name) == "" {
			return ErrUnsupportedSemanticEvent
		}
		if len(v.Resources) == 0 {
			v.Resources = json.RawMessage(`[]`)
		}
		return s.PutWorldSystem(narrative.WorldSystem{TimelineID: tid, ID: v.SystemID, Name: v.Name, Kind: v.Kind, Description: v.Description, Resources: v.Resources, Status: v.Status, Version: max(v.Version, 1)})
	case "upsert_world_rule", "archive_world_rule":
		var v struct {
			RuleID           string          `json:"ruleId"`
			SystemID         string          `json:"systemId"`
			Title            string          `json:"title"`
			Category         string          `json:"category"`
			Severity         string          `json:"severity"`
			Statement        string          `json:"statement"`
			Preconditions    json.RawMessage `json:"preconditions"`
			Costs            json.RawMessage `json:"costs"`
			ForbiddenResults json.RawMessage `json:"forbiddenResults"`
			Exceptions       json.RawMessage `json:"exceptions"`
			Tags             json.RawMessage `json:"tags"`
			Visibility       string          `json:"visibility"`
			Status           string          `json:"status"`
			ExceptionOf      string          `json:"exceptionOf"`
			Source           string          `json:"source"`
			Evidence         string          `json:"evidence"`
			Version          int64           `json:"version"`
			EstablishedAtSeq int64           `json:"establishedAtSeq"`
		}
		if json.Unmarshal(p.After, &v) != nil || strings.TrimSpace(v.RuleID) == "" || strings.TrimSpace(v.SystemID) == "" || strings.TrimSpace(v.Statement) == "" {
			return ErrUnsupportedSemanticEvent
		}
		if v.EstablishedAtSeq < 1 {
			v.EstablishedAtSeq = seq
		}
		return s.PutWorldRule(narrative.WorldRule{TimelineID: tid, ID: v.RuleID, SystemID: v.SystemID, Title: v.Title, Category: v.Category, Severity: v.Severity, Statement: v.Statement, Preconditions: v.Preconditions, Costs: v.Costs, ForbiddenResults: v.ForbiddenResults, Exceptions: v.Exceptions, Tags: v.Tags, Visibility: v.Visibility, Status: v.Status, ExceptionOf: v.ExceptionOf, Source: v.Source, Evidence: v.Evidence, Version: max(v.Version, 1), EstablishedAtSeq: v.EstablishedAtSeq})
	case "update_world_resource":
		var v struct {
			ResourceID   string   `json:"resourceId"`
			SystemID     string   `json:"systemId"`
			OwnerType    string   `json:"ownerType"`
			OwnerID      id.ID    `json:"ownerId"`
			OwnerKey     string   `json:"ownerKey"`
			Name         string   `json:"name"`
			Unit         string   `json:"unit"`
			CurrentValue float64  `json:"currentValue"`
			MinValue     *float64 `json:"minValue"`
			MaxValue     *float64 `json:"maxValue"`
			Visibility   string   `json:"visibility"`
			Version      int64    `json:"version"`
		}
		if json.Unmarshal(p.After, &v) != nil || strings.TrimSpace(v.ResourceID) == "" || strings.TrimSpace(v.SystemID) == "" || strings.TrimSpace(v.Name) == "" {
			return ErrUnsupportedSemanticEvent
		}
		return s.PutWorldResource(narrative.WorldResourceState{TimelineID: tid, ResourceID: v.ResourceID, SystemID: v.SystemID, OwnerType: v.OwnerType, OwnerID: v.OwnerID, OwnerKey: v.OwnerKey, Name: v.Name, Unit: v.Unit, Current: v.CurrentValue, Minimum: v.MinValue, Maximum: v.MaxValue, Visibility: v.Visibility, Version: max(v.Version, 1)})
	case "update_instruction":
		var v struct {
			InstructionID id.ID
			Text          string
			Scope         narrative.DirectorScope
			Priority      narrative.DirectorPriority
			Status        string
			CreatedAtSeq  int64
		}
		if json.Unmarshal(p.After, &v) != nil || v.InstructionID.IsZero() || strings.TrimSpace(v.Text) == "" || (v.Status != "active" && v.Status != "cancelled") {
			return ErrUnsupportedSemanticEvent
		}
		current := s.DirectorInstructions[v.InstructionID]
		current.ID, current.TimelineID = v.InstructionID, tid
		current.Text, current.Scope, current.Priority = strings.TrimSpace(v.Text), v.Scope, v.Priority
		current.Status, current.CreatedAtSeq = v.Status, v.CreatedAtSeq
		if v.Status == "active" {
			current.ExpiresAt = nil
		}
		s.DirectorInstructions[v.InstructionID] = current
		return nil
	case "update_relationship":
		var v struct {
			RelationshipID  id.ID
			Status, Summary string
		}
		if json.Unmarshal(p.After, &v) != nil || v.RelationshipID.IsZero() {
			return ErrUnsupportedSemanticEvent
		}
		cur, ok := s.Relationships[v.RelationshipID]
		if !ok {
			return narrative.ErrNotFound
		}
		cur.Status = v.Status
		cur.Summary = v.Summary
		s.Relationships[v.RelationshipID] = cur
		return nil
	case "update_thread":
		var v struct {
			ThreadID       id.ID
			Title, Summary string
			Status         narrative.ThreadStatus
			Importance     float64
		}
		if json.Unmarshal(p.After, &v) != nil || v.ThreadID.IsZero() {
			return ErrUnsupportedSemanticEvent
		}
		cur, ok := s.Threads[v.ThreadID]
		if !ok {
			return narrative.ErrNotFound
		}
		cur.Title = v.Title
		cur.Summary = v.Summary
		cur.Status = v.Status
		cur.Importance = v.Importance
		cur.LastTouchedAtSeq = seq
		s.Threads[v.ThreadID] = cur
		return nil
	default:
		return ErrUnsupportedSemanticEvent
	}
}
