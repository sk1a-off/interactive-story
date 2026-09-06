package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/application/llmpolicy"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationmetrics"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

var (
	ErrMalformedProposal = errors.New("malformed generation proposal")
	ErrRejectedProposal  = errors.New("generation proposal rejected by policy")
)

type CanonAppender interface {
	AppendSemantic(context.Context, timeline.ID, int64, []event.PendingEvent) ([]event.StoredEvent, error)
}

type Phase string

const (
	PhaseQueued       Phase = "queued"
	PhaseInterpreting Phase = "interpreting"
	PhaseDirecting    Phase = "directing"
	PhasePacing       Phase = "pacing"
	PhaseWriting      Phase = "writing"
	PhaseDraftReady   Phase = "draft_ready"
	PhaseFinalizing   Phase = "finalizing"
	PhaseEvaluating   Phase = "evaluating"
	PhaseWorld        Phase = "world"
	PhaseJournal      Phase = "journal"
	PhaseObjectives   Phase = "objectives"
	PhaseChoices      Phase = "choices"
	PhaseValidating   Phase = "validating"
	PhaseCommitting   Phase = "committing"
	PhaseCompleted    Phase = "completed"
	PhaseRetrying     Phase = "retrying"
	PhaseFailed       Phase = "failed"
)

type Update struct {
	Sequence     int64       `json:"sequence"`
	GenerationID id.ID       `json:"generationId"`
	TimelineID   timeline.ID `json:"timelineId,omitempty"`
	Phase        Phase       `json:"phase"`
	Revision     int64       `json:"revision,omitempty"`
	Provisional  bool        `json:"provisional,omitempty"`
	TextDelta    string      `json:"textDelta,omitempty"`
	Error        string      `json:"error,omitempty"`
}
type Sink interface{ Publish(context.Context, Update) }

type PlayerAction struct {
	StoryID      id.ID       `json:"storyId"`
	TimelineID   timeline.ID `json:"timelineId"`
	ExpectedHead int64       `json:"expectedHead"`
	Text         string      `json:"text"`
}

type interpretation struct {
	Intent string `json:"intent"`
}
type direction struct {
	Goal string `json:"goal"`
}
type pacingDecision struct {
	Transition   string `json:"transition"`
	ChapterTitle string `json:"chapterTitle,omitempty"`
	ChapterGoal  string `json:"chapterGoal,omitempty"`
	ChapterTone  string `json:"chapterTone,omitempty"`
	SceneGoal    string `json:"sceneGoal,omitempty"`
	SceneMood    string `json:"sceneMood,omitempty"`
	LocationID   string `json:"locationId,omitempty"`
}
type objectiveChange struct {
	Operation         string `json:"operation"`
	ObjectiveID       string `json:"objectiveId,omitempty"`
	Reference         string `json:"reference,omitempty"`
	ParentObjectiveID string `json:"parentObjectiveId,omitempty"`
	ParentReference   string `json:"parentReference,omitempty"`
	Scope             string `json:"scope,omitempty"`
	Kind              string `json:"kind,omitempty"`
	QuestType         string `json:"questType,omitempty"`
	Title             string `json:"title,omitempty"`
	Description       string `json:"description,omitempty"`
	SuccessCriteria   string `json:"successCriteria,omitempty"`
	Status            string `json:"status,omitempty"`
	Progress          int    `json:"progress,omitempty"`
	Evidence          string `json:"evidence,omitempty"`
	EvidenceQuote     string `json:"evidenceQuote,omitempty"`
}
type journalChange struct {
	Operation     string   `json:"operation"`
	EntryID       string   `json:"entryId,omitempty"`
	Category      string   `json:"category,omitempty"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	Quantity      *int     `json:"quantity,omitempty"`
	Level         string   `json:"level,omitempty"`
	Status        string   `json:"status,omitempty"`
	Evidence      string   `json:"evidence,omitempty"`
	EvidenceQuote string   `json:"evidenceQuote,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}
type worldChange struct {
	Type           string `json:"type"`
	ID             string `json:"id,omitempty"`
	CharacterID    string `json:"characterId,omitempty"`
	LocationID     string `json:"locationId,omitempty"`
	Name           string `json:"name"`
	Age            int    `json:"age,omitempty"`
	Role           string `json:"role,omitempty"`
	Personality    string `json:"personality,omitempty"`
	Relationship   string `json:"relationship,omitempty"`
	VisualAnchorEn string `json:"visualAnchorEn,omitempty"`
	Mood           string `json:"mood,omitempty"`
	CurrentGoal    string `json:"currentGoal,omitempty"`
	Description    string `json:"description,omitempty"`
	EvidenceQuote  string `json:"evidenceQuote,omitempty"`
}
type choice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type proposal struct {
	Text    string          `json:"text"`
	Choices []choice        `json:"choices"`
	Events  []proposedEvent `json:"events"`
}
type proposedEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type InstructionSource interface {
	ActiveInstructions(context.Context, timeline.ID) ([]narrative.DirectorInstruction, error)
}

type Pipeline struct {
	LLM             aiport.StoryLLM
	MaxRepairs      int
	Canon           CanonAppender
	Instructions    InstructionSource
	Targets         generationtarget.Source
	Sink            Sink
	Metrics         func(generationmetrics.Metrics)
	ContextShadow   ShadowContext
	Planner         TurnPlanner
	Extractor       CanonExtractor
	ProvisionalBeat bool
}

func (p Pipeline) Run(ctx context.Context, generationID id.ID, a PlayerAction) (result []event.StoredEvent, runErr error) {
	runStarted := time.Now()
	metrics := generationmetrics.Metrics{}
	defer func() {
		metrics.TotalMS = time.Since(runStarted).Milliseconds()
		accounted := metrics.TargetLoadMS + metrics.ContextBuildMS + metrics.ContextShadowMS + metrics.PlannerMS + metrics.WriterMS + metrics.PostWriterMS + metrics.ValidationMS + metrics.CommitMS
		if metrics.TotalMS > accounted {
			metrics.UnattributedMS = metrics.TotalMS - accounted
		}
		metrics.Succeeded = runErr == nil
		if runErr != nil {
			metrics.ErrorText = runErr.Error()
		}
		if p.Metrics != nil {
			p.Metrics(metrics)
		}
	}()
	pub := func(ph Phase, delta, errText string) {
		if p.Sink != nil {
			p.Sink.Publish(ctx, Update{GenerationID: generationID, TimelineID: a.TimelineID, Phase: ph, TextDelta: delta, Error: errText})
		}
	}
	pub(PhaseQueued, "", "")
	fail := func(err error) ([]event.StoredEvent, error) { pub(PhaseFailed, "", err.Error()); return nil, err }
	a.Text = strings.TrimSpace(a.Text)
	if a.Text == "" || a.TimelineID == "" {
		return fail(ErrRejectedProposal)
	}
	if p.Targets == nil {
		return fail(ErrRejectedProposal)
	}
	targetLoadStarted := time.Now()
	target, err := p.Targets.CurrentWriteTarget(ctx, a.TimelineID)
	metrics.TargetLoadMS = time.Since(targetLoadStarted).Milliseconds()
	if err != nil {
		return fail(err)
	}
	// World-rule data may still exist on timelines created by the removed
	// feature. Keep it out of every model context so legacy rows cannot affect
	// new prose or decisions.
	target.WorldSystems = nil
	target.WorldRules = nil
	target.WorldResources = nil
	contextBuildStarted := time.Now()
	contextNoObjectives := contextWithout(target, "objectives")
	contextNoObjectivesJournal := contextWithout(target, "objectives", "heroJournal")
	contextNoWorld := contextWithout(target, "initialCast", "world")
	contextNoJournal := contextWithout(target, "heroJournal")
	quests := questHierarchy(target.Objectives)
	metrics.ContextBuildMS = time.Since(contextBuildStarted).Milliseconds()
	if p.ContextShadow != nil {
		shadowStarted := time.Now()
		shadowResult, shadowErr := p.ContextShadow.Build(ctx, a, target)
		metrics.ContextShadowMS = time.Since(shadowStarted).Milliseconds()
		metrics.ShadowRetrievedCount = shadowResult.RetrievedCount
		metrics.ShadowRetrievalError = shadowResult.RetrievalError
		if shadowErr != nil {
			metrics.ShadowRetrievalError = shadowErr.Error()
		}
	}

	plannerStarted := time.Now()
	var activeInstructions []narrative.DirectorInstruction
	if p.Instructions != nil {
		var err error
		activeInstructions, err = p.Instructions.ActiveInstructions(ctx, a.TimelineID)
		if err != nil {
			return fail(err)
		}
	}
	pub(PhaseInterpreting, "", "")
	var in interpretation
	var dir direction
	var pacing pacingDecision
	if p.Planner != nil {
		plan, planErr := p.Planner.Plan(ctx, a, target, activeInstructions)
		if planErr != nil {
			return fail(planErr)
		}
		in.Intent, dir.Goal, pacing = plan.Intent, plan.ImmediateGoal, pacingFromTurnPlan(plan)
		pub(PhaseDirecting, "", "")
		pub(PhasePacing, "", "")
	} else {
		if err := p.role(ctx, "action_interpreter", "v1", map[string]any{"action": a.Text, "context": target}, &in); err != nil {
			return fail(err)
		}
		if in.Intent == "" {
			return fail(ErrMalformedProposal)
		}
		if !intentPreservesAction(a.Text, in.Intent) {
			in.Intent = a.Text
			metrics.ActionIntentFallbackUsed = true
		}
		pub(PhaseDirecting, "", "")
		if err := p.role(ctx, "director", "v1", map[string]any{"intent": in.Intent, "directorInstructions": activeInstructions, "quests": quests, "heroJournal": target.Journal, "context": contextNoObjectivesJournal}, &dir); err != nil {
			return fail(err)
		}
		if dir.Goal == "" {
			return fail(ErrMalformedProposal)
		}
		pub(PhasePacing, "", "")
		if err := p.role(ctx, "pacing", "v1", map[string]any{"intent": in.Intent, "directorGoal": dir.Goal, "directorInstructions": activeInstructions, "quests": quests, "context": contextNoObjectives}, &pacing); err != nil {
			if !errors.Is(err, ErrMalformedProposal) {
				return fail(err)
			}
			// Structural hard limits below are authoritative. A malformed advisory
			// pacing response must not discard an otherwise valid player turn.
			pacing = pacingDecision{}
		}
	}
	pacing = normalizePacingDecision(target, dir.Goal, pacing)
	metrics.PlannerMS = time.Since(plannerStarted).Milliseconds()

	writerStarted := time.Now()
	pub(PhaseWriting, "", "")
	var draft struct {
		Text string `json:"text"`
	}
	writerInput := map[string]any{"intent": in.Intent, "goal": dir.Goal, "pacing": pacing, "directorInstructions": activeInstructions, "quests": quests, "heroJournal": target.Journal, "context": contextNoObjectivesJournal}
	if err := p.role(ctx, "writer", "v1", writerInput, &draft); err != nil {
		return fail(err)
	}
	if draft.Text == "" {
		return fail(ErrMalformedProposal)
	}
	if noveltyErr := narrativeNoveltyError(target.PreviousBeatText, draft.Text); noveltyErr != nil {
		// One bounded semantic rewrite. Do not send the rejected draft back: doing
		// so would reinforce the copied wording in the model context.
		writerInput["revisionInstruction"] = "The first draft repeated completed prose. Write a genuinely new continuation after previousBeatText. Do not recap, paraphrase or reuse its dialogue, actions, readouts, conclusions or wording. Introduce at least two observable state changes."
		var revised struct {
			Text string `json:"text"`
		}
		if err := p.role(ctx, "writer", "v1", writerInput, &revised); err != nil {
			return fail(err)
		}
		if revised.Text == "" {
			return fail(ErrMalformedProposal)
		}
		if err := narrativeNoveltyError(target.PreviousBeatText, revised.Text); err != nil {
			return fail(err)
		}
		draft.Text = revised.Text
	}
	pub(PhaseWriting, draft.Text, "")
	metrics.WriterMS = time.Since(writerStarted).Milliseconds()
	metrics.TimeToFirstStoryTextMS = time.Since(runStarted).Milliseconds()
	if p.ProvisionalBeat && p.Sink != nil {
		p.Sink.Publish(ctx, Update{GenerationID: generationID, TimelineID: a.TimelineID, Phase: PhaseDraftReady, Revision: 1, Provisional: true, TextDelta: draft.Text})
	}

	var worldResult struct {
		Changes []worldChange `json:"changes"`
	}
	var worldEvents []proposedEvent
	evaluateWorld := func() error {
		if err := p.role(ctx, "world_evaluator", "v1", map[string]any{"action": a.Text, "intent": in.Intent, "newBeat": draft.Text, "pacing": pacing, "currentCharacters": target.InitialCast, "currentWorld": target.World, "context": contextNoWorld}, &worldResult); err != nil {
			if !errors.Is(err, ErrMalformedProposal) {
				return err
			}
			worldResult.Changes = nil
		}
		preparedChanges, prepareErr := prepareWorldEvidence(draft.Text, worldResult.Changes)
		if prepareErr != nil {
			worldResult.Changes = nil
		} else {
			worldResult.Changes = preparedChanges
		}
		var validationErr error
		worldEvents, _, validationErr = validateWorldChanges(target, worldResult.Changes)
		if validationErr != nil {
			if !errors.Is(validationErr, ErrRejectedProposal) {
				return validationErr
			}
			worldEvents = nil
		}
		return nil
	}

	var journalResult struct {
		Changes []journalChange `json:"changes"`
	}
	var journalEvents []proposedEvent
	var normalizedJournalChanges []journalChange
	evaluateJournal := func() error {
		if err := p.role(ctx, "state_evaluator", "v1", map[string]any{
			"action": a.Text, "intent": in.Intent, "newBeat": draft.Text,
			"currentJournal": target.Journal, "bootstrapJournal": len(target.Journal) == 0, "context": contextNoJournal,
		}, &journalResult); err != nil {
			if !errors.Is(err, ErrMalformedProposal) {
				return err
			}
			journalResult.Changes = nil
		}
		preparedChanges, prepareErr := prepareJournalEvidence(draft.Text, target.Journal, journalResult.Changes)
		if prepareErr != nil {
			journalResult.Changes = nil
		} else {
			journalResult.Changes = preparedChanges
		}
		var validationErr error
		journalEvents, normalizedJournalChanges, validationErr = validateJournalChanges(target.Journal, journalResult.Changes)
		if validationErr != nil {
			if !errors.Is(validationErr, ErrRejectedProposal) {
				return validationErr
			}
			journalEvents, normalizedJournalChanges = nil, nil
		}
		return nil
	}

	var objectiveResult struct {
		Changes []objectiveChange `json:"changes"`
	}
	var objectiveEvents []proposedEvent
	var normalizedChanges []objectiveChange
	evaluateObjectives := func() error {
		if err := p.role(ctx, "quest_evaluator", "v1", map[string]any{
			"action": a.Text, "intent": in.Intent, "directorGoal": dir.Goal,
			"newBeat": draft.Text, "currentObjectives": target.Objectives, "quests": quests, "context": contextNoObjectives,
		}, &objectiveResult); err != nil {
			if !errors.Is(err, ErrMalformedProposal) {
				return err
			}
			objectiveResult.Changes = nil
		}
		preparedChanges, prepareErr := prepareObjectiveEvidence(draft.Text, target.Objectives, objectiveResult.Changes)
		if prepareErr != nil {
			objectiveResult.Changes = nil
		} else {
			objectiveResult.Changes = preparedChanges
		}
		var validationErr error
		objectiveEvents, normalizedChanges, validationErr = validateObjectiveChanges(target.Objectives, objectiveResult.Changes)
		if validationErr != nil {
			if !errors.Is(validationErr, ErrRejectedProposal) {
				return validationErr
			}
			objectiveEvents, normalizedChanges = nil, nil
		}
		return nil
	}
	evaluateExtraction := func() error {
		extraction, extractErr := p.Extractor.Extract(ctx, a, target, in.Intent, draft.Text, dir, pacing)
		if extractErr != nil {
			return extractErr
		}
		worldResult.Changes, extractErr = prepareWorldEvidence(draft.Text, extraction.WorldChanges)
		if extractErr != nil {
			return extractErr
		}
		worldEvents, _, extractErr = validateWorldChanges(target, worldResult.Changes)
		if extractErr != nil {
			return extractErr
		}
		journalResult.Changes, extractErr = prepareJournalEvidence(draft.Text, target.Journal, extraction.JournalChanges)
		if extractErr != nil {
			return extractErr
		}
		journalEvents, normalizedJournalChanges, extractErr = validateJournalChanges(target.Journal, journalResult.Changes)
		if extractErr != nil {
			return extractErr
		}
		objectiveResult.Changes, extractErr = prepareObjectiveEvidence(draft.Text, target.Objectives, extraction.ObjectiveChanges)
		if extractErr != nil {
			return extractErr
		}
		objectiveEvents, normalizedChanges, extractErr = validateObjectiveChanges(target.Objectives, objectiveResult.Changes)
		return extractErr
	}
	var choices struct {
		Choices []choice `json:"choices"`
	}
	var choiceLabels []string
	choicesFallbackUsed := false
	useChoiceFallback := func() {
		choices.Choices = deterministicChoiceFallback()
		choiceLabels, _ = fourChoiceLabels(choices.Choices)
		choicesFallbackUsed = true
	}
	evaluateChoices := func(includePendingChanges bool) error {
		var objectiveChanges []objectiveChange
		var journalChanges []journalChange
		if includePendingChanges {
			objectiveChanges = normalizedChanges
			journalChanges = normalizedJournalChanges
		}
		if err := p.role(ctx, "choices", "v1", map[string]any{"text": draft.Text, "requiredCount": 4, "objectives": target.Objectives, "quests": quests, "objectiveChanges": objectiveChanges, "heroJournal": target.Journal, "journalChanges": journalChanges, "context": contextNoObjectivesJournal}, &choices); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			useChoiceFallback()
			return nil
		}
		var ok bool
		choiceLabels, ok = fourChoiceLabels(choices.Choices)
		if ok {
			return nil
		}
		// One bounded semantic retry. This is separate from syntax repair: the
		// first response may be valid JSON but violate the exact-four contract.
		var retried struct {
			Choices []choice `json:"choices"`
		}
		if err := p.role(ctx, "choices", "v1", map[string]any{"text": draft.Text, "requiredCount": 4, "objectives": target.Objectives, "quests": quests, "objectiveChanges": objectiveChanges, "heroJournal": target.Journal, "journalChanges": journalChanges, "context": contextNoObjectivesJournal, "previousChoices": choices.Choices, "instruction": "Return exactly four distinct actionable choices."}, &retried); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			useChoiceFallback()
			return nil
		}
		choiceLabels, ok = fourChoiceLabels(retried.Choices)
		if !ok {
			useChoiceFallback()
			return nil
		}
		choices.Choices = retried.Choices
		return nil
	}

	postWriterStarted := time.Now()
	if p.ProvisionalBeat {
		pub(PhaseFinalizing, "", "")
	}
	if p.Extractor != nil {
		pub(PhaseEvaluating, "", "")
		pub(PhaseChoices, "", "")
		extractorDone, choicesDone := make(chan error, 1), make(chan error, 1)
		go func() { extractorDone <- evaluateExtraction() }()
		// Extracted mutations are deliberately excluded here: they are not Canon
		// yet, and Choices can derive the immediate next actions from the final
		// Beat plus authoritative state. This removes the false dependency and
		// lets both bounded post-Writer stages run concurrently.
		go func() { choicesDone <- evaluateChoices(false) }()
		extractorErr, choicesErr := <-extractorDone, <-choicesDone
		if extractorErr != nil {
			return fail(extractorErr)
		}
		if choicesErr != nil {
			return fail(choicesErr)
		}
	} else if llmpolicy.SupportsParallelRequests(p.LLM.Identity()) {
		pub(PhaseEvaluating, "", "")
		worldDone, journalDone, objectivesDone := make(chan error, 1), make(chan error, 1), make(chan error, 1)
		go func() { worldDone <- evaluateWorld() }()
		go func() { journalDone <- evaluateJournal() }()
		go func() { objectivesDone <- evaluateObjectives() }()
		journalErr, objectivesErr := <-journalDone, <-objectivesDone
		if journalErr != nil || objectivesErr != nil {
			// Join the remaining request before returning so no provider call keeps
			// mutating shared result state after this turn has ended.
			worldErr := <-worldDone
			if journalErr != nil {
				return fail(journalErr)
			}
			if objectivesErr != nil {
				return fail(objectivesErr)
			}
			return fail(worldErr)
		}
		// Choices depend on journal and quest changes, but not on world entity
		// extraction, so they may overlap the still-running world branch.
		pub(PhaseChoices, "", "")
		if err := evaluateChoices(true); err != nil {
			<-worldDone
			return fail(err)
		}
		if err := <-worldDone; err != nil {
			return fail(err)
		}
	} else {
		pub(PhaseWorld, "", "")
		if err := evaluateWorld(); err != nil {
			return fail(err)
		}
		pub(PhaseJournal, "", "")
		if err := evaluateJournal(); err != nil {
			return fail(err)
		}
		pub(PhaseObjectives, "", "")
		if err := evaluateObjectives(); err != nil {
			return fail(err)
		}
		pub(PhaseChoices, "", "")
		if err := evaluateChoices(true); err != nil {
			return fail(err)
		}
	}
	metrics.PostWriterMS = time.Since(postWriterStarted).Milliseconds()
	metrics.ChoicesFallbackUsed = choicesFallbackUsed

	validationStarted := time.Now()
	beatID, err := id.New()
	if err != nil {
		return fail(err)
	}
	writeSceneID, beatPosition := target.SceneID, target.NextBeatPosition
	lifecycleEvents := []proposedEvent{}
	if pacing.Transition == "new_scene" || pacing.Transition == "new_chapter" {
		lifecycleEvents = append(lifecycleEvents, proposedEvent{Type: "scene_completed", Payload: mustJSON(map[string]any{"sceneId": target.SceneID, "status": "completed"})})
		chapterID := target.ChapterID
		sceneNumber := target.SceneNumber + 1
		if pacing.Transition == "new_chapter" {
			lifecycleEvents = append(lifecycleEvents, proposedEvent{Type: "chapter_completed", Payload: mustJSON(map[string]any{"chapterId": target.ChapterID, "status": "completed"})})
			chapterID, err = id.New()
			if err != nil {
				return fail(err)
			}
			sceneNumber = 1
			lifecycleEvents = append(lifecycleEvents, proposedEvent{Type: "chapter_created", Payload: mustJSON(map[string]any{"chapterId": chapterID, "number": target.ChapterNumber + 1, "title": pacing.ChapterTitle, "goal": pacing.ChapterGoal, "tone": pacing.ChapterTone, "status": "active"})})
		}
		writeSceneID, err = id.New()
		if err != nil {
			return fail(err)
		}
		beatPosition = 1
		lifecycleEvents = append(lifecycleEvents, proposedEvent{Type: "scene_started", Payload: mustJSON(map[string]any{"sceneId": writeSceneID, "chapterId": chapterID, "locationId": pacing.LocationID, "number": sceneNumber, "mood": pacing.SceneMood, "goal": pacing.SceneGoal, "status": "awaiting_player"})})
	}
	prop := proposal{Text: draft.Text, Choices: choices.Choices, Events: []proposedEvent{{Type: "player_action_attempted", Payload: mustJSON(map[string]any{"text": a.Text, "intent": in.Intent})}}}
	prop.Events = append(prop.Events, lifecycleEvents...)
	prop.Events = append(prop.Events, proposedEvent{Type: "beat_committed", Payload: mustJSON(map[string]any{"beatId": beatID, "sceneId": writeSceneID, "position": beatPosition, "kind": beatKindForPacing(pacing), "text": draft.Text, "status": "committed", "directorGoal": dir.Goal})})
	prop.Events = append(prop.Events, worldEvents...)
	prop.Events = append(prop.Events, journalEvents...)
	prop.Events = append(prop.Events, objectiveEvents...)
	if expired := expiringInstructionIDs(activeInstructions, pacing.Transition); len(expired) > 0 {
		prop.Events = append(prop.Events, proposedEvent{Type: "director_instructions_expired", Payload: mustJSON(map[string]any{"instructionIds": expired, "transition": pacing.Transition})})
	}
	prop.Events = append(prop.Events, proposedEvent{Type: "choices_ready", Payload: mustJSON(map[string]any{"beatId": beatID, "choices": choiceLabels})})
	pub(PhaseValidating, "", "")
	pending := make([]event.PendingEvent, 0, len(prop.Events))
	gid := event.GenerationID(generationID)
	for _, e := range prop.Events {
		// Only application policy can turn an AI proposal into Canon events.
		if e.Type == "" || !json.Valid(e.Payload) {
			return fail(ErrRejectedProposal)
		}
		pending = append(pending, event.PendingEvent{Type: e.Type, SchemaVersion: 1, Payload: e.Payload, GenerationID: &gid})
	}
	metrics.ValidationMS = time.Since(validationStarted).Milliseconds()
	pub(PhaseCommitting, "", "")
	commitStarted := time.Now()
	committed, err := p.Canon.AppendSemantic(ctx, a.TimelineID, a.ExpectedHead, pending)
	metrics.CommitMS = time.Since(commitStarted).Milliseconds()
	if err != nil {
		return fail(err)
	}
	pub(PhaseCompleted, "", "")
	return committed, nil
}

type questGroup struct {
	Quest  generationtarget.Objective   `json:"quest"`
	Stages []generationtarget.Objective `json:"stages"`
}

func normalizePacingDecision(target generationtarget.Target, directorGoal string, value pacingDecision) pacingDecision {
	value.Transition = strings.ToLower(strings.TrimSpace(value.Transition))
	value.ChapterTitle = strings.TrimSpace(value.ChapterTitle)
	value.ChapterGoal = strings.TrimSpace(value.ChapterGoal)
	value.ChapterTone = strings.TrimSpace(value.ChapterTone)
	value.SceneGoal = strings.TrimSpace(value.SceneGoal)
	value.SceneMood = strings.TrimSpace(value.SceneMood)
	value.LocationID = strings.TrimSpace(value.LocationID)
	if value.Transition != "continue_scene" && value.Transition != "new_scene" && value.Transition != "new_chapter" {
		value.Transition = "continue_scene"
	}
	// Structural hard stops prevent a weak/local model from leaving a whole
	// novel inside one database scene or chapter forever.
	if target.ChapterBeatCount >= 60 {
		value.Transition = "new_chapter"
	} else if target.NextBeatPosition > 12 && value.Transition == "continue_scene" {
		value.Transition = "new_scene"
	}
	if value.Transition == "new_chapter" {
		if value.ChapterTitle == "" {
			value.ChapterTitle = fmt.Sprintf("Глава %d", target.ChapterNumber+1)
		}
		if value.ChapterGoal == "" {
			value.ChapterGoal = directorGoal
		}
	}
	if value.Transition == "new_scene" || value.Transition == "new_chapter" {
		if value.SceneGoal == "" {
			value.SceneGoal = directorGoal
		}
	} else {
		value.ChapterTitle, value.ChapterGoal, value.ChapterTone = "", "", ""
		value.SceneGoal, value.SceneMood, value.LocationID = "", "", ""
	}
	if value.LocationID != "" && !activeLocationID(target.World, value.LocationID) {
		value.LocationID = ""
	}
	return value
}

func activeLocationID(world json.RawMessage, candidate string) bool {
	if _, err := id.Parse(candidate); err != nil {
		return false
	}
	var payload struct {
		Locations []struct {
			ID string `json:"id"`
		} `json:"locations"`
	}
	if json.Unmarshal(world, &payload) != nil {
		return false
	}
	for _, location := range payload.Locations {
		if location.ID == candidate {
			return true
		}
	}
	return false
}

func beatKindForPacing(value pacingDecision) narrative.BeatKind {
	if value.Transition == "new_scene" || value.Transition == "new_chapter" {
		return narrative.BeatTransition
	}
	return narrative.BeatMixed
}

func expiringInstructionIDs(values []narrative.DirectorInstruction, transition string) []id.ID {
	out := make([]id.ID, 0, len(values))
	for _, value := range values {
		expires := value.Scope == narrative.ScopeNextBeat || value.Scope == narrative.ScopeTemporary
		if transition == "new_scene" || transition == "new_chapter" {
			expires = expires || value.Scope == narrative.ScopeScene
		}
		if transition == "new_chapter" {
			expires = expires || value.Scope == narrative.ScopeChapter
		}
		if expires && !value.ID.IsZero() {
			out = append(out, value.ID)
		}
	}
	return out
}

func validateWorldChanges(target generationtarget.Target, changes []worldChange) ([]proposedEvent, []worldChange, error) {
	if len(changes) > 4 {
		return nil, nil, ErrRejectedProposal
	}
	var cast struct {
		Characters []worldChange `json:"characters"`
	}
	_ = json.Unmarshal(target.InitialCast, &cast)
	var world struct {
		Locations []worldChange `json:"locations"`
	}
	_ = json.Unmarshal(target.World, &world)
	charactersByID, charactersByName := map[string]worldChange{}, map[string]worldChange{}
	for _, character := range cast.Characters {
		if character.CharacterID == "" {
			character.CharacterID = character.ID
		}
		charactersByID[character.CharacterID] = character
		charactersByName[strings.ToLower(strings.TrimSpace(character.Name))] = character
	}
	locationsByID, locationsByName := map[string]worldChange{}, map[string]worldChange{}
	for _, location := range world.Locations {
		if location.LocationID == "" {
			location.LocationID = location.ID
		}
		locationsByID[location.LocationID] = location
		locationsByName[strings.ToLower(strings.TrimSpace(location.Name))] = location
	}
	events := make([]proposedEvent, 0, len(changes))
	normalized := make([]worldChange, 0, len(changes))
	changed := map[string]struct{}{}
	for _, change := range changes {
		change.Type = strings.ToLower(strings.TrimSpace(change.Type))
		change.CharacterID = strings.TrimSpace(change.CharacterID)
		change.LocationID = strings.TrimSpace(change.LocationID)
		change.Name = strings.TrimSpace(change.Name)
		change.Role = strings.TrimSpace(change.Role)
		change.Personality = strings.TrimSpace(change.Personality)
		change.Relationship = strings.TrimSpace(change.Relationship)
		change.VisualAnchorEn = strings.TrimSpace(change.VisualAnchorEn)
		change.Mood = strings.TrimSpace(change.Mood)
		change.CurrentGoal = strings.TrimSpace(change.CurrentGoal)
		change.Description = strings.TrimSpace(change.Description)
		nameKey := strings.ToLower(change.Name)
		switch change.Type {
		case "upsert_character":
			base, exists := charactersByID[change.CharacterID]
			if change.CharacterID != "" && !exists {
				return nil, nil, ErrRejectedProposal
			}
			if !exists && nameKey != "" {
				base, exists = charactersByName[nameKey]
				if exists {
					change.CharacterID = base.CharacterID
				}
			}
			if change.Name == "" {
				change.Name = base.Name
			}
			if change.Name == "" {
				return nil, nil, ErrRejectedProposal
			}
			if exists {
				if change.Age == 0 {
					change.Age = base.Age
				}
				change.Role = firstNonEmpty(change.Role, base.Role)
				change.Personality = firstNonEmpty(change.Personality, base.Personality)
				change.Relationship = firstNonEmpty(change.Relationship, base.Relationship)
				change.VisualAnchorEn = firstNonEmpty(change.VisualAnchorEn, base.VisualAnchorEn)
				change.Mood = firstNonEmpty(change.Mood, base.Mood)
				change.CurrentGoal = firstNonEmpty(change.CurrentGoal, base.CurrentGoal)
			} else {
				if change.Age == 0 {
					change.Age = 18
				}
				fresh, err := id.New()
				if err != nil {
					return nil, nil, err
				}
				change.CharacterID = fresh.String()
			}
			if change.Age < 1 || change.Age > 150 {
				return nil, nil, ErrRejectedProposal
			}
			key := "character|" + change.CharacterID
			if _, duplicate := changed[key]; duplicate {
				return nil, nil, ErrRejectedProposal
			}
			changed[key] = struct{}{}
			if exists && sameWorldCharacter(base, change) {
				continue
			}
			events = append(events, proposedEvent{Type: "character_world_upserted", Payload: mustJSON(map[string]any{"characterId": change.CharacterID, "name": change.Name, "age": change.Age, "kind": "persistent_npc", "role": change.Role, "personality": change.Personality, "relationship": change.Relationship, "visualAnchorEn": change.VisualAnchorEn, "mood": change.Mood, "currentGoal": change.CurrentGoal, "active": true})})
		case "upsert_location":
			base, exists := locationsByID[change.LocationID]
			if change.LocationID != "" && !exists {
				return nil, nil, ErrRejectedProposal
			}
			if !exists && nameKey != "" {
				base, exists = locationsByName[nameKey]
				if exists {
					change.LocationID = base.LocationID
				}
			}
			if change.Name == "" {
				change.Name = base.Name
			}
			if change.Name == "" {
				return nil, nil, ErrRejectedProposal
			}
			if exists {
				change.Description = firstNonEmpty(change.Description, base.Description)
				change.VisualAnchorEn = firstNonEmpty(change.VisualAnchorEn, base.VisualAnchorEn)
			} else {
				if change.Description == "" {
					return nil, nil, ErrRejectedProposal
				}
				fresh, err := id.New()
				if err != nil {
					return nil, nil, err
				}
				change.LocationID = fresh.String()
			}
			key := "location|" + change.LocationID
			if _, duplicate := changed[key]; duplicate {
				return nil, nil, ErrRejectedProposal
			}
			changed[key] = struct{}{}
			if exists && sameWorldLocation(base, change) {
				continue
			}
			events = append(events, proposedEvent{Type: "location_world_upserted", Payload: mustJSON(map[string]any{"locationId": change.LocationID, "name": change.Name, "description": change.Description, "visualAnchorEn": change.VisualAnchorEn, "active": true})})
		default:
			return nil, nil, ErrRejectedProposal
		}
		normalized = append(normalized, change)
	}
	return events, normalized, nil
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func sameWorldCharacter(left, right worldChange) bool {
	return left.Name == right.Name && left.Age == right.Age && left.Role == right.Role && left.Personality == right.Personality && left.Relationship == right.Relationship && left.VisualAnchorEn == right.VisualAnchorEn && left.Mood == right.Mood && left.CurrentGoal == right.CurrentGoal
}

func sameWorldLocation(left, right worldChange) bool {
	return left.Name == right.Name && left.Description == right.Description && left.VisualAnchorEn == right.VisualAnchorEn
}

func validateJournalChanges(current []generationtarget.JournalEntry, changes []journalChange) ([]proposedEvent, []journalChange, error) {
	if len(changes) > 8 {
		return nil, nil, ErrRejectedProposal
	}
	byID := make(map[string]generationtarget.JournalEntry, len(current))
	byName := make(map[string]string, len(current))
	for _, entry := range current {
		if entry.ID == "" || (entry.Category != "ability" && entry.Category != "attribute" && entry.Category != "item" && entry.Category != "currency") {
			continue
		}
		byID[entry.ID] = entry
		byName[entry.Category+"|"+strings.ToLower(strings.TrimSpace(entry.Name))] = entry.ID
	}
	events := make([]proposedEvent, 0, len(changes))
	normalized := make([]journalChange, 0, len(changes))
	changedIDs := map[string]struct{}{}
	for _, change := range changes {
		change.Operation = strings.ToLower(strings.TrimSpace(change.Operation))
		change.EntryID = strings.TrimSpace(change.EntryID)
		change.Category = strings.ToLower(strings.TrimSpace(change.Category))
		change.Name = strings.TrimSpace(change.Name)
		change.Description = strings.TrimSpace(change.Description)
		change.Level = strings.TrimSpace(change.Level)
		change.Status = strings.ToLower(strings.TrimSpace(change.Status))
		change.Evidence = strings.TrimSpace(change.Evidence)
		change.Tags = normalizeJournalTags(change.Tags)
		eventType := "journal_entry_updated"
		status := "active"
		if change.Operation == "create" {
			if (change.Category != "ability" && change.Category != "attribute" && change.Category != "item" && change.Category != "currency") || change.Name == "" || change.Evidence == "" || (change.Status != "" && change.Status != "active") {
				return nil, nil, ErrRejectedProposal
			}
			nameKey := change.Category + "|" + strings.ToLower(change.Name)
			if _, duplicate := byName[nameKey]; duplicate {
				// Repeating an already known ability or item should not break the
				// entire story generation. The existing journal entry remains canon.
				continue
			}
			quantity := 0
			if change.Category == "item" || change.Category == "currency" {
				if change.Category == "item" {
					quantity = 1
				}
				if change.Quantity != nil {
					quantity = *change.Quantity
				}
				if !validJournalQuantity(change.Category, quantity) {
					return nil, nil, ErrRejectedProposal
				}
			} else if change.Level == "" && change.Category == "ability" {
				change.Level = "освоено"
			} else if change.Level == "" && change.Category == "attribute" {
				return nil, nil, ErrRejectedProposal
			}
			change.Quantity = intPointer(quantity)
			change.Status = status
			fresh, err := id.New()
			if err != nil {
				return nil, nil, err
			}
			change.EntryID = fresh.String()
			byID[change.EntryID] = generationtarget.JournalEntry{ID: change.EntryID, Category: change.Category, Name: change.Name, Description: change.Description, Quantity: quantity, Level: change.Level, Status: status, Evidence: change.Evidence, Tags: change.Tags}
			byName[nameKey] = change.EntryID
			eventType = "journal_entry_created"
		} else {
			if _, duplicate := changedIDs[change.EntryID]; duplicate {
				return nil, nil, ErrRejectedProposal
			}
			changedIDs[change.EntryID] = struct{}{}
			base, ok := byID[change.EntryID]
			if !ok || base.Status != "active" || change.Evidence == "" {
				return nil, nil, ErrRejectedProposal
			}
			change.Category = base.Category
			if change.Name == "" {
				change.Name = base.Name
			}
			if change.Description == "" {
				change.Description = base.Description
			}
			if change.Level == "" {
				change.Level = base.Level
			}
			if len(change.Tags) == 0 {
				change.Tags = base.Tags
			}
			quantity := base.Quantity
			switch change.Operation {
			case "update":
				if (change.Category == "item" || change.Category == "currency") && change.Quantity != nil {
					quantity = *change.Quantity
				}
				if (change.Category == "item" || change.Category == "currency") && !validJournalQuantity(change.Category, quantity) {
					return nil, nil, ErrRejectedProposal
				}
			case "remove":
				status = "inactive"
				if change.Category == "item" || change.Category == "currency" {
					quantity = 0
				}
			default:
				return nil, nil, ErrRejectedProposal
			}
			change.Quantity = intPointer(quantity)
			change.Status = status
		}
		events = append(events, proposedEvent{Type: eventType, Payload: mustJSON(journalPayload(change))})
		normalized = append(normalized, change)
	}
	return events, normalized, nil
}

func validJournalQuantity(category string, quantity int) bool {
	switch category {
	case "item":
		return quantity >= 1 && quantity <= 9999
	case "currency":
		return quantity >= 0 && quantity <= 999999999
	default:
		return false
	}
}

func normalizeJournalTags(tags []string) []string {
	out := make([]string, 0, min(len(tags), 6))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		key := strings.ToLower(tag)
		if tag == "" {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
		if len(out) == 6 {
			break
		}
	}
	return out
}

func intPointer(value int) *int { return &value }

func journalPayload(change journalChange) map[string]any {
	quantity := 0
	if change.Quantity != nil {
		quantity = *change.Quantity
	}
	return map[string]any{"entryId": change.EntryID, "category": change.Category, "name": change.Name, "description": change.Description, "quantity": quantity, "level": change.Level, "status": change.Status, "evidence": change.Evidence, "tags": change.Tags}
}

func questHierarchy(objectives []generationtarget.Objective) []questGroup {
	groups := make([]questGroup, 0)
	byID := map[string]int{}
	for _, objective := range objectives {
		if objective.Scope != "global" {
			continue
		}
		byID[objective.ID] = len(groups)
		groups = append(groups, questGroup{Quest: objective, Stages: []generationtarget.Objective{}})
	}
	for _, objective := range objectives {
		if objective.Scope != "minor" {
			continue
		}
		if index, ok := byID[objective.ParentObjectiveID]; ok {
			groups[index].Stages = append(groups[index].Stages, objective)
		}
	}
	return groups
}

func validateObjectiveChanges(current []generationtarget.Objective, changes []objectiveChange) ([]proposedEvent, []objectiveChange, error) {
	if len(changes) > 6 {
		return nil, nil, ErrRejectedProposal
	}
	byID := make(map[string]generationtarget.Objective, len(current))
	for _, objective := range current {
		if objective.ID != "" && !objective.Virtual {
			if objective.Kind == "" {
				if objective.Scope == "global" {
					objective.Kind = "quest"
				} else {
					objective.Kind = "task"
				}
			}
			if objective.QuestType == "" {
				objective.QuestType = "main"
			}
			byID[objective.ID] = objective
		}
	}
	events := make([]proposedEvent, 0, len(changes))
	normalized := make([]objectiveChange, 0, len(changes))
	references := map[string]string{}
	ignoredReferences := map[string]struct{}{}
	changedIDs := map[string]struct{}{}
	affectedParents := map[string]struct{}{}
	explicitParentChanges := map[string]struct{}{}
	for _, change := range changes {
		change.Operation = strings.ToLower(strings.TrimSpace(change.Operation))
		change.ObjectiveID = strings.TrimSpace(change.ObjectiveID)
		change.Reference = strings.TrimSpace(change.Reference)
		change.ParentObjectiveID = strings.TrimSpace(change.ParentObjectiveID)
		change.ParentReference = strings.TrimSpace(change.ParentReference)
		change.Scope = strings.ToLower(strings.TrimSpace(change.Scope))
		change.Kind = strings.ToLower(strings.TrimSpace(change.Kind))
		change.QuestType = strings.ToLower(strings.TrimSpace(change.QuestType))
		change.Title = strings.TrimSpace(change.Title)
		change.Description = strings.TrimSpace(change.Description)
		change.SuccessCriteria = strings.TrimSpace(change.SuccessCriteria)
		change.Evidence = strings.TrimSpace(change.Evidence)
		status := "active"
		eventType := "objective_updated"
		if change.Operation == "create" {
			if change.Title == "" || change.SuccessCriteria == "" || (change.Scope != "global" && change.Scope != "minor") || change.Progress < 0 || change.Progress > 100 {
				return nil, nil, ErrRejectedProposal
			}
			if change.Scope == "global" {
				if change.Kind == "" {
					change.Kind = "quest"
				}
				if change.Kind != "quest" || change.ParentObjectiveID != "" || change.ParentReference != "" {
					return nil, nil, ErrRejectedProposal
				}
				// A newly discovered optional plot line is safer than silently
				// promoting it into the main story. The prompt asks for an explicit
				// value, while this default keeps older/local models compatible.
				if change.QuestType == "" {
					change.QuestType = "side"
				}
				if change.QuestType != "main" && change.QuestType != "side" {
					return nil, nil, ErrRejectedProposal
				}
			} else {
				if change.Kind == "" {
					change.Kind = "task"
				}
				if change.Kind != "task" && change.Kind != "event" && change.Kind != "milestone" {
					return nil, nil, ErrRejectedProposal
				}
				_, existingParent := byID[change.ParentObjectiveID]
				if !existingParent && change.ParentReference != "" {
					resolvedParentID, resolved := references[change.ParentReference]
					if !resolved {
						if _, intentionallyIgnored := ignoredReferences[change.ParentReference]; intentionallyIgnored {
							continue
						}
						return nil, nil, ErrRejectedProposal
					}
					change.ParentObjectiveID = resolvedParentID
				}
				parent, ok := byID[change.ParentObjectiveID]
				if !ok || parent.Scope != "global" || parent.Status != "active" {
					return nil, nil, ErrRejectedProposal
				}
				if change.QuestType != "" && change.QuestType != parent.QuestType {
					return nil, nil, ErrRejectedProposal
				}
				change.QuestType = parent.QuestType
			}
			switch change.Status {
			case "", "active":
				change.Status, status = "active", "active"
				if change.Progress == 100 {
					return nil, nil, ErrRejectedProposal
				}
			case "completed", "failed":
				// A new objective whose outcome is already known is historical noise,
				// not a goal for the player. Ignore it without failing the story turn.
				continue
			default:
				return nil, nil, ErrRejectedProposal
			}
			if duplicateID := objectiveWithTitle(byID, change.Scope, change.ParentObjectiveID, change.Title); duplicateID != "" {
				if change.Scope == "global" && change.Reference != "" {
					references[change.Reference] = duplicateID
				}
				continue
			}
			if change.Scope == "global" && (activeQuestCount(byID, "") >= 5 || activeQuestCount(byID, change.QuestType) >= 3) {
				if change.Reference != "" {
					ignoredReferences[change.Reference] = struct{}{}
				}
				continue
			}
			if change.Scope == "minor" && activeStageCount(byID, change.ParentObjectiveID) >= 3 {
				continue
			}
			fresh, err := id.New()
			if err != nil {
				return nil, nil, err
			}
			change.ObjectiveID = fresh.String()
			if change.Reference != "" {
				if _, duplicate := references[change.Reference]; duplicate {
					return nil, nil, ErrRejectedProposal
				}
				references[change.Reference] = change.ObjectiveID
			}
			eventType = "objective_created"
			byID[change.ObjectiveID] = generationtarget.Objective{ID: change.ObjectiveID, ParentObjectiveID: change.ParentObjectiveID, Scope: change.Scope, Kind: change.Kind, QuestType: change.QuestType, Title: change.Title, Description: change.Description, SuccessCriteria: change.SuccessCriteria, Status: status, Progress: change.Progress, Evidence: change.Evidence}
		} else {
			if _, duplicate := changedIDs[change.ObjectiveID]; duplicate {
				return nil, nil, ErrRejectedProposal
			}
			changedIDs[change.ObjectiveID] = struct{}{}
			base, ok := byID[change.ObjectiveID]
			if !ok || base.Status != "active" {
				return nil, nil, ErrRejectedProposal
			}
			change.Scope = base.Scope
			change.Kind = base.Kind
			change.QuestType = base.QuestType
			change.ParentObjectiveID = base.ParentObjectiveID
			if change.Title == "" {
				change.Title = base.Title
			}
			if change.Description == "" {
				change.Description = base.Description
			}
			if change.SuccessCriteria == "" {
				change.SuccessCriteria = base.SuccessCriteria
			}
			switch change.Operation {
			case "progress":
				if change.Progress < base.Progress || change.Progress > 99 {
					return nil, nil, ErrRejectedProposal
				}
			case "complete":
				if change.Evidence == "" {
					return nil, nil, ErrRejectedProposal
				}
				status, change.Status, change.Progress = "completed", "completed", 100
			case "fail":
				if change.Evidence == "" {
					return nil, nil, ErrRejectedProposal
				}
				status, change.Status, change.Progress = "failed", "failed", base.Progress
			default:
				return nil, nil, ErrRejectedProposal
			}
			base.Title, base.Description, base.SuccessCriteria = change.Title, change.Description, change.SuccessCriteria
			base.Status, base.Progress, base.Evidence = status, change.Progress, change.Evidence
			byID[change.ObjectiveID] = base
		}
		if change.Scope == "minor" && change.ParentObjectiveID != "" {
			affectedParents[change.ParentObjectiveID] = struct{}{}
		}
		if change.Scope == "global" {
			explicitParentChanges[change.ObjectiveID] = struct{}{}
		}
		payload := objectivePayload(change, status)
		events = append(events, proposedEvent{Type: eventType, Payload: mustJSON(payload)})
		normalized = append(normalized, change)
	}
	for _, objective := range byID {
		if objective.Scope != "global" || objective.Status == "active" {
			continue
		}
		for _, child := range byID {
			if child.ParentObjectiveID == objective.ID && child.Status == "active" {
				return nil, nil, ErrRejectedProposal
			}
		}
	}
	for parentID := range affectedParents {
		if _, explicit := explicitParentChanges[parentID]; explicit {
			continue
		}
		parent, ok := byID[parentID]
		if !ok || parent.Status != "active" {
			continue
		}
		total, count := 0, 0
		for _, child := range byID {
			if child.ParentObjectiveID != parentID {
				continue
			}
			count++
			if child.Status == "completed" {
				total += 100
			} else {
				total += child.Progress
			}
		}
		if count == 0 {
			continue
		}
		progress := total / count
		if progress > 99 {
			progress = 99
		}
		if progress <= parent.Progress {
			continue
		}
		change := objectiveChange{Operation: "progress", ObjectiveID: parent.ID, Scope: parent.Scope, Kind: parent.Kind, QuestType: parent.QuestType, Title: parent.Title, Description: parent.Description, SuccessCriteria: parent.SuccessCriteria, Status: "active", Progress: progress}
		events = append(events, proposedEvent{Type: "objective_updated", Payload: mustJSON(objectivePayload(change, "active"))})
		normalized = append(normalized, change)
	}
	return events, normalized, nil
}

func objectiveWithTitle(values map[string]generationtarget.Objective, scope, parentID, title string) string {
	key := strings.ToLower(strings.TrimSpace(title))
	for objectiveID, objective := range values {
		if objective.Scope == scope && objective.ParentObjectiveID == parentID && strings.ToLower(strings.TrimSpace(objective.Title)) == key && objective.Status != "failed" {
			return objectiveID
		}
	}
	return ""
}

func activeQuestCount(values map[string]generationtarget.Objective, questType string) int {
	count := 0
	for _, objective := range values {
		if objective.Scope == "global" && objective.Status == "active" && (questType == "" || objective.QuestType == questType) {
			count++
		}
	}
	return count
}

func activeStageCount(values map[string]generationtarget.Objective, parentID string) int {
	count := 0
	for _, objective := range values {
		if objective.Scope == "minor" && objective.ParentObjectiveID == parentID && objective.Status == "active" {
			count++
		}
	}
	return count
}

func objectivePayload(change objectiveChange, status string) map[string]any {
	return map[string]any{"objectiveId": change.ObjectiveID, "parentObjectiveId": change.ParentObjectiveID, "scope": change.Scope, "kind": change.Kind, "questType": change.QuestType, "title": change.Title, "description": change.Description, "successCriteria": change.SuccessCriteria, "status": status, "progress": change.Progress, "evidence": change.Evidence}
}

func fourChoiceLabels(choices []choice) ([]string, bool) {
	if len(choices) != 4 {
		return nil, false
	}
	out := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	for _, c := range choices {
		label := strings.TrimSpace(c.Label)
		if label == "" {
			return nil, false
		}
		key := strings.ToLower(label)
		if _, exists := seen[key]; exists {
			return nil, false
		}
		seen[key] = struct{}{}
		out = append(out, label)
	}
	return out, true
}

func deterministicChoiceFallback() []choice {
	return []choice{
		{ID: "continue_carefully", Label: "Осторожно продолжить начатое действие"},
		{ID: "observe", Label: "Внимательнее осмотреться вокруг"},
		{ID: "ask", Label: "Обратиться к ближайшему собеседнику"},
		{ID: "reconsider", Label: "Отступить и выбрать другой подход"},
	}
}

func (p Pipeline) role(ctx context.Context, role, version string, input any, out any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	resp, err := p.LLM.Generate(ctx, aiport.StoryRequest{Role: role, PromptVersion: version, Input: raw})
	if err != nil {
		return err
	}
	if err := json.Unmarshal(resp.Output, out); err == nil {
		return nil
	}
	// Controlled repair is bounded and never sees or writes Canon directly.
	for attempt := 0; attempt < p.MaxRepairs; attempt++ {
		repairInput, _ := json.Marshal(map[string]any{"role": role, "invalidOutput": string(resp.Output), "contract": "return valid JSON only"})
		resp, err = p.LLM.Generate(ctx, aiport.StoryRequest{Role: "structured_repair", PromptVersion: "v1", Input: repairInput, Repair: true})
		if err != nil {
			return err
		}
		if err := json.Unmarshal(resp.Output, out); err == nil {
			return nil
		}
	}
	return fmt.Errorf("%w: role %s", ErrMalformedProposal, role)
}
func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
