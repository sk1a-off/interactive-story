package narrative

import (
	"encoding/json"
	"errors"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

var (
	ErrInvalidCharacterAge = errors.New("character age must be between 18 and 150")
	ErrSelfRelationship    = errors.New("relationship cannot target the same character")
	ErrInvalidConfidence   = errors.New("confidence must be between 0 and 1")
	ErrInvalidItemState    = errors.New("item cannot have both owner and location")
	ErrDuplicate           = errors.New("entity already exists")
	ErrNotFound            = errors.New("entity not found")
	ErrTimelineMismatch    = errors.New("entity belongs to another timeline")
)

type CharacterKind string

const (
	CharacterPlayer            CharacterKind = "player"
	CharacterPersistentNPC     CharacterKind = "persistent_npc"
	CharacterTemporaryPromoted CharacterKind = "temporary_promoted"
)

type Character struct {
	ID       id.ID
	StoryID  id.ID
	Kind     CharacterKind
	Name     string
	AdultAge int
	Core     json.RawMessage
}

func NewCharacter(storyID, characterID id.ID, kind CharacterKind, name string, adultAge int) (Character, error) {
	if adultAge < 1 || adultAge > 150 {
		return Character{}, ErrInvalidCharacterAge
	}
	return Character{ID: characterID, StoryID: storyID, Kind: kind, Name: name, AdultAge: adultAge, Core: json.RawMessage(`{}`)}, nil
}

type CharacterState struct {
	TimelineID  id.ID
	CharacterID id.ID
	LocationID  id.ID
	Mood        string
	CurrentGoal string
	Active      bool
	Appearance  json.RawMessage
	// Profile contains branch-local Director overrides such as effective name,
	// role, personality, relationship and visualAnchorEn.
	Profile json.RawMessage
	Version int64
}

type LocationState struct {
	TimelineID, LocationID id.ID
	Name, Description      string
	VisualProfile          json.RawMessage
	Active                 bool
	Version                int64
}
type ValueType string

const (
	ValueNumber ValueType = "number"
	ValueBool   ValueType = "bool"
	ValueString ValueType = "string"
	ValueEnum   ValueType = "enum"
	ValueTags   ValueType = "tags"
)

type StatOwnerType string

const (
	OwnerCharacter    StatOwnerType = "character"
	OwnerRelationship StatOwnerType = "relationship"
	OwnerWorld        StatOwnerType = "world"
	OwnerItem         StatOwnerType = "item"
	OwnerScene        StatOwnerType = "scene"
)

type Stat struct {
	TimelineID    id.ID
	OwnerType     StatOwnerType
	OwnerID       id.ID
	Key           string
	ValueType     ValueType
	Value         json.RawMessage
	TargetValue   json.RawMessage
	EvolutionMode string
	EvolutionMeta json.RawMessage
}
type Relationship struct {
	ID              id.ID
	TimelineID      id.ID
	FromCharacterID id.ID
	ToCharacterID   id.ID
	Status          string
	Summary         string
}

func NewRelationship(timelineID, relationshipID, fromID, toID id.ID) (Relationship, error) {
	if fromID == toID {
		return Relationship{}, ErrSelfRelationship
	}
	return Relationship{ID: relationshipID, TimelineID: timelineID, FromCharacterID: fromID, ToCharacterID: toID}, nil
}

type Chapter struct {
	ID, TimelineID            id.ID
	Number                    int
	Title, Goal, Tone, Status string
	Summary                   json.RawMessage
}
type Scene struct {
	ID, ChapterID, LocationID id.ID
	Number                    int
	StoryTime                 json.RawMessage
	Mood, Goal, Status        string
}
type BeatKind string

const (
	BeatImage       BeatKind = "image"
	BeatDialogue    BeatKind = "dialogue"
	BeatDescription BeatKind = "description"
	BeatMixed       BeatKind = "mixed"
	BeatReaction    BeatKind = "reaction"
	BeatTransition  BeatKind = "transition"
	BeatChoice      BeatKind = "choice"
	BeatContinue    BeatKind = "continue"
)

type Beat struct {
	ID, SceneID  id.ID
	Position     int
	Kind         BeatKind
	Content      json.RawMessage
	Status       string
	IsActive     bool
	GenerationID id.ID
}
type FactStatus string

const (
	FactActive      FactStatus = "active"
	FactInvalidated FactStatus = "invalidated"
	FactSuperseded  FactStatus = "superseded"
)

type Fact struct {
	ID, TimelineID   id.ID
	SubjectType      string
	SubjectID        id.ID
	Predicate        string
	Object           json.RawMessage
	Status           FactStatus
	SourceEventID    id.ID
	ValidFromSeq     int64
	InvalidatedAtSeq *int64
}
type Knowledge struct {
	TimelineID, FactID, KnowerCharacterID id.ID
	Confidence                            float64
	LearnedAtEventSeq                     int64
	InvalidatedAtEventSeq                 *int64
}

func NewKnowledge(timelineID, factID, knowerID id.ID, confidence float64, seq int64) (Knowledge, error) {
	if confidence < 0 || confidence > 1 {
		return Knowledge{}, ErrInvalidConfidence
	}
	return Knowledge{TimelineID: timelineID, FactID: factID, KnowerCharacterID: knowerID, Confidence: confidence, LearnedAtEventSeq: seq}, nil
}

type BeliefStance string

const (
	Believes    BeliefStance = "believes"
	Suspects    BeliefStance = "suspects"
	Doubts      BeliefStance = "doubts"
	Disbelieves BeliefStance = "disbelieves"
)

type Belief struct {
	ID, TimelineID, CharacterID id.ID
	SubjectType                 string
	SubjectID                   id.ID
	Predicate                   string
	Object                      json.RawMessage
	Stance                      BeliefStance
	Confidence                  float64
	Status                      FactStatus
	SourceEventSeq              *int64
}

// WorldSystem and WorldRule are Timeline-scoped Canon. Stable textual IDs make
// rules addressable from prompts, audits and Director edits without exposing a
// database UUID or losing the author's own terminology.
type WorldSystem struct {
	TimelineID  id.ID
	ID          string
	Name        string
	Kind        string
	Description string
	Resources   json.RawMessage
	Status      string
	Version     int64
}

type WorldRule struct {
	TimelineID       id.ID
	ID               string
	SystemID         string
	Title            string
	Category         string
	Severity         string
	Statement        string
	Preconditions    json.RawMessage
	Costs            json.RawMessage
	ForbiddenResults json.RawMessage
	Exceptions       json.RawMessage
	Tags             json.RawMessage
	Visibility       string
	Status           string
	ExceptionOf      string
	Source           string
	Evidence         string
	Version          int64
	EstablishedAtSeq int64
}

type WorldResourceState struct {
	TimelineID id.ID
	ResourceID string
	SystemID   string
	OwnerType  string
	OwnerID    id.ID
	OwnerKey   string
	Name       string
	Unit       string
	Current    float64
	Minimum    *float64
	Maximum    *float64
	Visibility string
	Version    int64
}

func NewBelief(timelineID, beliefID, characterID id.ID, stance BeliefStance, confidence float64) (Belief, error) {
	if confidence < 0 || confidence > 1 {
		return Belief{}, ErrInvalidConfidence
	}
	return Belief{ID: beliefID, TimelineID: timelineID, CharacterID: characterID, Stance: stance, Confidence: confidence, Status: FactActive}, nil
}

type Item struct {
	ID, StoryID       id.ID
	Name, Description string
}
type ItemState struct {
	TimelineID, ItemID, OwnerCharacterID, LocationID id.ID
	Condition                                        string
	State                                            json.RawMessage
	Version                                          int64
}

func NewItemState(timelineID, itemID, ownerID, locationID id.ID) (ItemState, error) {
	if !ownerID.IsZero() && !locationID.IsZero() {
		return ItemState{}, ErrInvalidItemState
	}
	return ItemState{TimelineID: timelineID, ItemID: itemID, OwnerCharacterID: ownerID, LocationID: locationID, State: json.RawMessage(`{}`), Version: 1}, nil
}
func (s ItemState) TransferTo(ownerID id.ID) ItemState {
	s.OwnerCharacterID = ownerID
	s.LocationID = ""
	s.Version++
	return s
}
func (s ItemState) PlaceAt(locationID id.ID) ItemState {
	s.LocationID = locationID
	s.OwnerCharacterID = ""
	s.Version++
	return s
}

type ThreadStatus string

const (
	ThreadOpen      ThreadStatus = "open"
	ThreadDormant   ThreadStatus = "dormant"
	ThreadResolving ThreadStatus = "resolving"
	ThreadClosed    ThreadStatus = "closed"
	ThreadAbandoned ThreadStatus = "abandoned"
)

type StoryThread struct {
	ID, TimelineID                    id.ID
	Title, Summary                    string
	Importance                        float64
	Status                            ThreadStatus
	Metadata                          json.RawMessage
	IntroducedAtSeq, LastTouchedAtSeq int64
}

// Objective is stored in the existing timeline-scoped story_threads projection,
// but has an explicit Canon contract of its own. This keeps objectives replayable
// and branch-local instead of turning them into disposable UI state.
type ObjectiveScope string

const (
	ObjectiveGlobal ObjectiveScope = "global"
	ObjectiveMinor  ObjectiveScope = "minor"
)

type ObjectiveKind string

const (
	ObjectiveQuest     ObjectiveKind = "quest"
	ObjectiveTask      ObjectiveKind = "task"
	ObjectiveEvent     ObjectiveKind = "event"
	ObjectiveMilestone ObjectiveKind = "milestone"
)

type QuestType string

const (
	QuestMain QuestType = "main"
	QuestSide QuestType = "side"
)

type ObjectiveStatus string

const (
	ObjectiveActive    ObjectiveStatus = "active"
	ObjectiveCompleted ObjectiveStatus = "completed"
	ObjectiveFailed    ObjectiveStatus = "failed"
)

type Objective struct {
	ID, TimelineID                id.ID
	ParentObjectiveID             id.ID
	Scope                         ObjectiveScope
	Kind                          ObjectiveKind
	QuestType                     QuestType
	Title, Description            string
	SuccessCriteria, Evidence     string
	Status                        ObjectiveStatus
	Progress                      int
	IntroducedAtSeq, UpdatedAtSeq int64
}
type DirectorScope string

const (
	ScopeNextBeat   DirectorScope = "next_beat"
	ScopeScene      DirectorScope = "scene"
	ScopeChapter    DirectorScope = "chapter"
	ScopeTemporary  DirectorScope = "temporary"
	ScopePersistent DirectorScope = "persistent"
)

type DirectorPriority string

const (
	PriorityLow    DirectorPriority = "low"
	PriorityNormal DirectorPriority = "normal"
	PriorityHigh   DirectorPriority = "high"
	PriorityHard   DirectorPriority = "hard"
)

type DirectorInstruction struct {
	ID           id.ID
	TimelineID   id.ID
	Text         string
	Scope        DirectorScope
	Priority     DirectorPriority
	Status       string
	CreatedAtSeq int64
	ExpiresAt    json.RawMessage
}
