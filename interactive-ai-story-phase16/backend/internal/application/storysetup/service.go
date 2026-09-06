package storysetup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/local/interactive-ai-story/backend/internal/application/llmpolicy"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/setup"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/setuprepo"
)

var (
	ErrIncompleteDraft          = errors.New("setup generation did not return all required components")
	ErrAlreadyStarted           = errors.New("story already started")
	ErrInvalidAssistInstruction = errors.New("setup assistant instruction is invalid")
	ErrNoEditableComponents     = errors.New("setup assistant has no editable components in scope")
	ErrInvalidAssistProposal    = errors.New("setup assistant returned an invalid proposal")
	ErrStoryNotFound            = errors.New("story not found")
	ErrIdeaTooLong              = errors.New("story idea is longer than 40000 characters")
	ErrGenerationActive         = errors.New("story setup generation is already running")
)

const (
	ideaSoftLimitChars    = 12000
	ideaHardLimitChars    = 40000
	canonicalContextChars = 12000
)

type LLMFactory func(context.Context) (aiport.StoryLLM, error)

type Service struct {
	Repo       setuprepo.Repository
	LLM        aiport.StoryLLM
	LLMFactory LLMFactory
	Now        func() time.Time
	OwnerID    story.UserID
}
type CreateCommand struct {
	Title, Idea string
	Tags        []string
}
type GenerateCommand struct {
	StoryID story.ID
	Keys    []setup.ComponentKey
}

type AssistCommand struct {
	StoryID     story.ID
	Keys        []setup.ComponentKey
	Instruction string
	Action      string
	Target      AssistTarget
	// Drafts are browser-side, not-yet-canonical component payloads. They are
	// context only: Assist never writes them or its proposal to the repository.
	Drafts map[setup.ComponentKey]json.RawMessage
}

type AssistTarget struct {
	Path            string `json:"path,omitempty"`
	MatchField      string `json:"matchField,omitempty"`
	MatchValue      string `json:"matchValue,omitempty"`
	ChildPath       string `json:"childPath,omitempty"`
	ChildMatchField string `json:"childMatchField,omitempty"`
	ChildMatchValue string `json:"childMatchValue,omitempty"`
}

type AssistOperation struct {
	Component       setup.ComponentKey `json:"component"`
	Operation       string             `json:"operation"`
	Path            string             `json:"path"`
	MatchField      string             `json:"matchField,omitempty"`
	MatchValue      string             `json:"matchValue,omitempty"`
	ChildPath       string             `json:"childPath,omitempty"`
	ChildMatchField string             `json:"childMatchField,omitempty"`
	ChildMatchValue string             `json:"childMatchValue,omitempty"`
	Value           json.RawMessage    `json:"value,omitempty"`
}

type AssistChange struct {
	Key          setup.ComponentKey `json:"key"`
	Before       json.RawMessage    `json:"before"`
	After        json.RawMessage    `json:"after"`
	ChangedPaths []string           `json:"changedPaths"`
	Operations   []AssistOperation  `json:"operations"`
}

type AssistResult struct {
	GenerationID event.GenerationID `json:"generationId"`
	Summary      string             `json:"summary"`
	Changes      []AssistChange     `json:"changes"`
}

type assistProposal struct {
	Summary    string            `json:"summary"`
	Operations []AssistOperation `json:"operations"`
}
type ManualEditCommand struct {
	StoryID story.ID
	Key     setup.ComponentKey
	Payload json.RawMessage
	Locked  *bool
}
type StartResult struct {
	Timeline          timeline.Timeline `json:"timeline"`
	SceneID           id.ID             `json:"sceneId"`
	BeatID            id.ID             `json:"beatId"`
	ImageGenerationID *id.ID            `json:"imageGenerationId,omitempty"`
}

type StoryListItem struct {
	ID              story.ID           `json:"id"`
	Title           string             `json:"title"`
	Description     string             `json:"description"`
	Status          story.Status       `json:"status"`
	ReadyComponents int                `json:"readyComponents"`
	TotalComponents int                `json:"totalComponents"`
	LatestTimeline  *StoryListTimeline `json:"latestTimeline,omitempty"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
}

type StoryListTimeline struct {
	ID           timeline.ID     `json:"id"`
	Name         string          `json:"name"`
	Status       timeline.Status `json:"status"`
	HeadEventSeq int64           `json:"headEventSeq"`
}

func (s Service) List(ctx context.Context) ([]StoryListItem, error) {
	rows, err := s.Repo.ListStories(ctx, s.OwnerID)
	if err != nil {
		return nil, err
	}
	items := make([]StoryListItem, 0, len(rows))
	for _, row := range rows {
		item := StoryListItem{
			ID:              row.Story.ID,
			Title:           row.Story.Title,
			Description:     row.Story.Description,
			Status:          row.Story.Status,
			ReadyComponents: row.ReadyComponents,
			TotalComponents: len(setup.AllKeys),
			CreatedAt:       row.Story.CreatedAt,
			UpdatedAt:       row.LastActivityAt,
		}
		if row.LatestTimeline != nil {
			item.LatestTimeline = &StoryListTimeline{
				ID:           row.LatestTimeline.ID,
				Name:         row.LatestTimeline.Name,
				Status:       row.LatestTimeline.Status,
				HeadEventSeq: row.LatestTimeline.HeadEventSeq,
			}
		}
		items = append(items, item)
	}
	return items, nil
}

func (s Service) Delete(ctx context.Context, storyID story.ID) error {
	deleted, err := s.Repo.DeleteStory(ctx, storyID, s.OwnerID)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrStoryNotFound
	}
	return nil
}

func (s Service) Create(ctx context.Context, c CreateCommand) (story.Story, error) {
	if utf8.RuneCountInString(strings.TrimSpace(c.Idea)) > ideaHardLimitChars {
		return story.Story{}, ErrIdeaTooLong
	}
	sid, e := id.New()
	if e != nil {
		return story.Story{}, e
	}
	now := s.Now()
	st, e := story.New(story.ID(sid), s.OwnerID, c.Title, now)
	if e != nil {
		return story.Story{}, e
	}
	st.Description = strings.TrimSpace(c.Idea)
	if e = s.Repo.CreateStory(ctx, st, c.Tags); e != nil {
		return story.Story{}, e
	}
	return st, nil
}
func (s Service) Generate(ctx context.Context, c GenerateCommand) ([]setup.Component, error) {
	st, e := s.Repo.GetStory(ctx, c.StoryID)
	if e != nil {
		return nil, e
	}
	if st.Status == story.StatusDeleted {
		return nil, ErrStoryNotFound
	}
	existing, e := s.Repo.ListComponents(ctx, c.StoryID)
	if e != nil {
		return nil, e
	}
	byKey := map[setup.ComponentKey]setup.Component{}
	for _, v := range existing {
		byKey[v.Key] = v
	}
	requested := map[setup.ComponentKey]bool{}
	if len(c.Keys) == 0 {
		for _, k := range setup.AllKeys {
			current, exists := byKey[k]
			if !exists || current.Status != "ready" {
				requested[k] = true
			}
		}
	} else {
		for _, k := range c.Keys {
			requested[k] = true
		}
	}
	for k := range requested {
		if v, ok := byKey[k]; ok && v.Locked {
			delete(requested, k)
		}
	}
	if len(requested) == 0 {
		return existing, nil
	}
	gidRaw, e := id.New()
	if e != nil {
		return nil, e
	}
	gid := event.GenerationID(gidRaw)
	llm := s.LLM
	if s.LLMFactory != nil {
		llm, e = s.LLMFactory(ctx)
		if e != nil {
			return nil, e
		}
	}
	if llm == nil {
		return nil, aiport.ErrUnavailable
	}
	ident := llm.Identity()
	if e = s.Repo.CreateSetupGeneration(ctx, gid, c.StoryID, ident.Provider, ident.Model, ident.Profile); e != nil {
		if errors.Is(e, setuprepo.ErrGenerationActive) {
			return nil, ErrGenerationActive
		}
		return nil, e
	}
	// Local llama.cpp executes one request at a time to protect its single GPU
	// worker. Hosted models use an explicit dependency graph: a component starts
	// as soon as its own Canon dependencies are complete.
	var failedKey setup.ComponentKey
	if llmpolicy.SupportsParallelRequests(ident) {
		failedKey, e = s.generateSetupDAG(ctx, llm, st, c.StoryID, gid, requested, byKey)
	} else {
		failedKey, e = s.generateSetupSequential(ctx, llm, st, c.StoryID, gid, requested, byKey)
	}
	if e != nil {
		msg := fmt.Sprintf("invalid %s after retry: %v", failedKey, e)
		finishSetupGeneration(ctx, s.Repo, gid, "failed", &msg)
		var persistenceError *setupPersistenceError
		if errors.As(e, &persistenceError) {
			return nil, persistenceError.err
		}
		return nil, ErrIncompleteDraft
	}
	finishSetupGeneration(ctx, s.Repo, gid, "completed", nil)
	return s.Repo.ListComponents(ctx, c.StoryID)
}

type setupPersistenceError struct{ err error }

func (e *setupPersistenceError) Error() string {
	return "setup component persistence failed: " + e.err.Error()
}
func (e *setupPersistenceError) Unwrap() error { return e.err }

func (s Service) generateSetupSequential(ctx context.Context, llm aiport.StoryLLM, st story.Story, storyID story.ID, gid event.GenerationID, requested map[setup.ComponentKey]bool, byKey map[setup.ComponentKey]setup.Component) (setup.ComponentKey, error) {
	for _, key := range setup.AllKeys {
		if !requested[key] {
			continue
		}
		raw, err := generateSetupKey(ctx, llm, st, key, buildCanonicalContext(byKey, key))
		if err != nil {
			return key, err
		}
		component := generatedSetupComponent(s, storyID, key, gid, raw, byKey)
		if err = s.Repo.UpsertComponents(ctx, []setup.Component{component}); err != nil {
			return key, &setupPersistenceError{err: err}
		}
		byKey[key] = component
	}
	return "", nil
}

func (s Service) generateSetupDAG(ctx context.Context, llm aiport.StoryLLM, st story.Story, storyID story.ID, gid event.GenerationID, requested map[setup.ComponentKey]bool, byKey map[setup.ComponentKey]setup.Component) (setup.ComponentKey, error) {
	dependencies := map[setup.ComponentKey][]setup.ComponentKey{
		setup.StoryBible:       {},
		setup.Player:           {setup.StoryBible},
		setup.World:            {setup.StoryBible},
		setup.InitialCast:      {setup.StoryBible, setup.Player, setup.World},
		setup.VisualBible:      {setup.StoryBible, setup.Player, setup.World, setup.InitialCast},
		setup.InitialQuests:    {setup.StoryBible, setup.Player, setup.World, setup.InitialCast},
		setup.OpeningSituation: {setup.StoryBible, setup.Player, setup.World, setup.InitialCast, setup.InitialQuests},
	}
	type result struct {
		key setup.ComponentKey
		raw json.RawMessage
		err error
	}
	graphCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan result, len(requested))
	pending := make(map[setup.ComponentKey]bool, len(requested))
	completed := make(map[setup.ComponentKey]bool, len(requested))
	for key := range requested {
		pending[key] = true
	}
	running := 0
	launchReady := func() {
		for _, key := range setup.AllKeys {
			if !pending[key] {
				continue
			}
			ready := true
			for _, dependency := range dependencies[key] {
				if requested[dependency] && !completed[dependency] {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			delete(pending, key)
			running++
			componentContext := buildCanonicalContext(byKey, key)
			go func(componentKey setup.ComponentKey, canon map[string]json.RawMessage) {
				raw, err := generateSetupKey(graphCtx, llm, st, componentKey, canon)
				results <- result{key: componentKey, raw: raw, err: err}
			}(key, componentContext)
		}
	}
	launchReady()
	var firstFailure result
	for running > 0 {
		value := <-results
		running--
		if value.err != nil {
			if firstFailure.err == nil {
				firstFailure = value
				cancel()
			}
			continue
		}
		if firstFailure.err != nil {
			continue
		}
		component := generatedSetupComponent(s, storyID, value.key, gid, value.raw, byKey)
		if err := s.Repo.UpsertComponents(ctx, []setup.Component{component}); err != nil {
			firstFailure = result{key: value.key, err: &setupPersistenceError{err: err}}
			cancel()
			continue
		}
		byKey[value.key] = component
		completed[value.key] = true
		launchReady()
	}
	if firstFailure.err != nil {
		return firstFailure.key, firstFailure.err
	}
	if len(pending) != 0 {
		return setup.StoryBible, errors.New("setup dependency graph did not make progress")
	}
	return "", nil
}

func generateSetupKey(ctx context.Context, llm aiport.StoryLLM, st story.Story, key setup.ComponentKey, componentContext map[string]json.RawMessage) (json.RawMessage, error) {
	switch key {
	case setup.WorldRules:
		return generateWorldRules(ctx, llm, st, componentContext)
	case setup.InitialQuests:
		return generateInitialQuests(ctx, llm, st, componentContext)
	case setup.OpeningSituation:
		return generateOpeningSituation(ctx, llm, st, componentContext)
	default:
		return generateSetupComponent(ctx, llm, st, key, componentContext)
	}
}

func generateWorldRules(ctx context.Context, llm aiport.StoryLLM, st story.Story, canon map[string]json.RawMessage) (json.RawMessage, error) {
	architecture, err := generateSetupFragment(ctx, llm, st, setup.WorldRules, canon, "systems", "Return world_rules.systems only. Include 1-5 named systems with stable lowercase ids, kind, concise description and 0-5 resources. A resource has id, name, unit, ownerScope (world or hero), optional initialValue, minValue and maxValue. Separate magic, neural interface and technology when their mechanisms differ.", 1800, worldRuleArchitectureIsValid)
	if err != nil {
		return nil, err
	}
	detailCanon := cloneRawMap(canon)
	detailCanon["worldSystems"] = architecture
	laws, err := generateSetupFragment(ctx, llm, st, setup.WorldRules, detailCanon, "laws", "Return world_rules.rules and world_rules.glossary only. Include 4-18 addressable rules. Every rule needs stable id, systemId from worldSystems, title, category, severity, statement, preconditions, costs, forbiddenResults, exceptions, tags, visibility and status. Make energy source, capabilities, limits, costs, failure modes and progression explicit. Hard laws must be testable; mysteries describe true hidden laws, not vague ideas.", 3200, worldRuleLawsAreValid)
	if err != nil {
		return nil, err
	}
	var systemsObject, lawsObject map[string]any
	if json.Unmarshal(architecture, &systemsObject) != nil || json.Unmarshal(laws, &lawsObject) != nil {
		return nil, setup.ErrInvalidComponent
	}
	merged := map[string]any{"systems": systemsObject["systems"], "rules": lawsObject["rules"], "glossary": lawsObject["glossary"]}
	raw, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	return normalizeGeneratedComponent(setup.WorldRules, raw)
}

func worldRuleArchitectureIsValid(object map[string]any) bool {
	systems, ok := object["systems"].([]any)
	if !ok || len(systems) < 1 || len(systems) > 5 {
		return false
	}
	for _, raw := range systems {
		system, ok := raw.(map[string]any)
		if !ok || strings.TrimSpace(fmt.Sprint(system["id"])) == "" || strings.TrimSpace(fmt.Sprint(system["name"])) == "" {
			return false
		}
	}
	return true
}

func worldRuleLawsAreValid(object map[string]any) bool {
	rules, ok := object["rules"].([]any)
	if !ok || len(rules) < 1 || len(rules) > 24 {
		return false
	}
	for _, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok || strings.TrimSpace(fmt.Sprint(rule["id"])) == "" || strings.TrimSpace(fmt.Sprint(rule["systemId"])) == "" || strings.TrimSpace(fmt.Sprint(rule["statement"])) == "" {
			return false
		}
	}
	return true
}

func generatedSetupComponent(s Service, storyID story.ID, key setup.ComponentKey, gid event.GenerationID, raw json.RawMessage, byKey map[setup.ComponentKey]setup.Component) setup.Component {
	revision := 1
	if old, ok := byKey[key]; ok {
		revision = old.Revision + 1
	}
	generationID := gid
	return setup.Component{StoryID: storyID, Key: key, Revision: revision, Source: "ai", Payload: raw, Locked: false, Status: "ready", GenerationID: &generationID, UpdatedAt: s.Now()}
}

const setupComponentAttempts = 2

func generateSetupComponent(ctx context.Context, llm aiport.StoryLLM, st story.Story, key setup.ComponentKey, existing map[string]json.RawMessage) (json.RawMessage, error) {
	raw, err := generateSetupFragment(ctx, llm, st, key, existing, "component", setupComponentOutputConstraints(key), setupComponentTokenLimit(key), func(map[string]any) bool { return true })
	if err != nil {
		return nil, err
	}
	return normalizeGeneratedComponent(key, raw)
}

func generateInitialQuests(ctx context.Context, llm aiport.StoryLLM, st story.Story, canon map[string]json.RawMessage) (json.RawMessage, error) {
	outline, err := generateSetupFragment(ctx, llm, st, setup.InitialQuests, canon, "quest_outlines", "Return only initial_quests.quests with 1-3 quest lines. Every quest must contain questType (main or side), title, concise description and observable successCriteria. Use main only for the central plot; use side for optional self-contained opportunities. Do not create stages in this phase.", 1400, func(object map[string]any) bool {
		quests, ok := object["quests"].([]any)
		return ok && len(quests) >= 1 && len(quests) <= 3 && questsHaveFields(quests, false)
	})
	if err != nil {
		return nil, err
	}
	detailCanon := cloneRawMap(canon)
	detailCanon["questOutline"] = outline
	details, err := generateSetupFragment(ctx, llm, st, setup.InitialQuests, detailCanon, "quest_stages", "Use the supplied questOutline. Return the same quest lines with the same questType and exactly 2-4 concise stages for each. Each stage needs kind (task, event, or milestone), title, description and observable successCriteria. Do not add or remove a quest line.", 2200, func(object map[string]any) bool {
		quests, ok := object["quests"].([]any)
		return ok && len(quests) >= 1 && len(quests) <= 3 && questsHaveFields(quests, true)
	})
	if err != nil {
		return nil, err
	}
	merged, err := mergeQuestPhases(outline, details)
	if err != nil {
		return nil, err
	}
	return normalizeGeneratedComponent(setup.InitialQuests, merged)
}

func generateOpeningSituation(ctx context.Context, llm aiport.StoryLLM, st story.Story, canon map[string]json.RawMessage) (json.RawMessage, error) {
	blueprint, err := generateSetupFragment(ctx, llm, st, setup.OpeningSituation, canon, "blueprint", "Plan the opening without finished prose. Return chapterTitle, chapterGoal, sceneGoal, exactly four short distinct player choices, and beats containing exactly 4-6 concise sequential paragraph events. The final beat must stop before the player's voluntary decision.", 1400, openingBlueprintIsValid)
	if err != nil {
		return nil, err
	}
	var blueprintObject map[string]any
	if json.Unmarshal(blueprint, &blueprintObject) != nil {
		return nil, setup.ErrInvalidComponent
	}
	blueprintObject = normalizedOpeningBlueprint(blueprintObject)
	blueprint, _ = json.Marshal(blueprintObject)

	firstCanon := cloneRawMap(canon)
	firstCanon["openingBlueprint"] = blueprint
	first, err := generateOpeningProsePart(ctx, llm, st, firstCanon, "prose_first", "Write only the first 2-3 substantial Russian paragraphs from openingBlueprint. Follow its beat order, introduce the scene, and do not include choices or goals.", true)
	if err != nil {
		return nil, err
	}
	secondCanon := cloneRawMap(firstCanon)
	firstRaw, _ := json.Marshal(map[string]any{"text": strings.Join(first, "\n\n")})
	secondCanon["openingFirstPart"] = firstRaw
	second, err := generateOpeningProsePart(ctx, llm, st, secondCanon, "prose_second", "Continue openingBlueprint with only the remaining 2-3 substantial Russian paragraphs. Continue after openingFirstPart without recapping or paraphrasing it. End at the planned actionable moment; do not include choices or goals.", false)
	if err != nil {
		return nil, err
	}
	paragraphs := append(append([]string{}, first...), second...)
	if !openingParagraphsAreValid(paragraphs) {
		return nil, setup.ErrInvalidComponent
	}
	delete(blueprintObject, "beats")
	delete(blueprintObject, "paragraphBeats")
	blueprintObject["text"] = strings.Join(paragraphs, "\n\n")
	combined, err := json.Marshal(blueprintObject)
	if err != nil {
		return nil, err
	}
	return normalizeGeneratedComponent(setup.OpeningSituation, combined)
}

func generateSetupFragment(ctx context.Context, llm aiport.StoryLLM, st story.Story, key setup.ComponentKey, canon map[string]json.RawMessage, phase, constraints string, maxTokens int, valid func(map[string]any) bool) (json.RawMessage, error) {
	var lastErr error
	for attempt := 0; attempt < setupComponentAttempts; attempt++ {
		inputPayload := map[string]any{
			"title":             st.Title,
			"idea":              generationIdea(st.Description),
			"components":        []string{string(key)},
			"activeComponent":   string(key),
			"phase":             phase,
			"outputConstraints": constraints,
			"canon":             canon,
		}
		if attempt > 0 {
			inputPayload["retryInstruction"] = "The previous response violated the JSON shape or phase constraints. Return a shorter complete JSON object for this phase only. Do not repeat prior prose."
		}
		input, err := json.Marshal(inputPayload)
		if err != nil {
			return nil, err
		}
		response, generateErr := llm.Generate(ctx, aiport.StoryRequest{Role: "setup_architect", PromptVersion: "v1", Input: input, MaxTokens: maxTokens})
		if generateErr != nil {
			lastErr = generateErr
			continue
		}
		raw, extractErr := extractSetupComponent(response.Output, key)
		if extractErr != nil {
			lastErr = extractErr
			continue
		}
		if phase == "component" {
			normalized, normalizeErr := normalizeGeneratedComponent(key, raw)
			if normalizeErr != nil {
				lastErr = normalizeErr
				continue
			}
			return normalized, nil
		}
		var object map[string]any
		if json.Unmarshal(raw, &object) != nil || object == nil || !valid(object) {
			lastErr = setup.ErrInvalidComponent
			continue
		}
		out, marshalErr := json.Marshal(object)
		return json.RawMessage(out), marshalErr
	}
	if lastErr == nil {
		lastErr = setup.ErrInvalidComponent
	}
	return nil, lastErr
}

func generateOpeningProsePart(ctx context.Context, llm aiport.StoryLLM, st story.Story, canon map[string]json.RawMessage, phase, constraints string, first bool) ([]string, error) {
	raw, err := generateSetupFragment(ctx, llm, st, setup.OpeningSituation, canon, phase, constraints, 1400, func(object map[string]any) bool {
		text, ok := object["text"].(string)
		return ok && len(openingPartParagraphs(text, first)) >= 2
	})
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil {
		return nil, setup.ErrInvalidComponent
	}
	return openingPartParagraphs(object["text"].(string), first), nil
}

func openingPartParagraphs(text string, first bool) []string {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n")
	paragraphs := nonEmptyParagraphs(text)
	if len(paragraphs) < 2 {
		paragraphs = nonEmptyLines(text)
	}
	if len(paragraphs) >= 4 && len(paragraphs) <= 6 {
		cut := (len(paragraphs) + 1) / 2
		if first {
			paragraphs = paragraphs[:cut]
		} else {
			paragraphs = paragraphs[cut:]
		}
	}
	if len(paragraphs) > 3 {
		paragraphs = paragraphs[:3]
	}
	if len(paragraphs) < 2 {
		return nil
	}
	return paragraphs
}

func finishSetupGeneration(ctx context.Context, repo setuprepo.Repository, gid event.GenerationID, status string, message *string) {
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = repo.FinishSetupGeneration(finishCtx, gid, status, message)
}

func generationIdea(idea string) map[string]any {
	trimmed := strings.TrimSpace(idea)
	length := utf8.RuneCountInString(trimmed)
	return map[string]any{
		"text":          compactIdea(trimmed, ideaSoftLimitChars),
		"condensed":     length > ideaSoftLimitChars,
		"originalChars": length,
	}
}

func compactIdea(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	contentLimit := max(1, limit-180)
	first := contentLimit * 5 / 12
	middle := contentLimit * 3 / 12
	last := contentLimit - first - middle
	middleStart := len(runes)/2 - middle/2
	return strings.TrimSpace(string(runes[:first])) +
		"\n\n[Средняя часть идеи сокращена до ключевого фрагмента]\n\n" +
		strings.TrimSpace(string(runes[middleStart:middleStart+middle])) +
		"\n\n[Далее сохранён финальный фрагмент идеи]\n\n" +
		strings.TrimSpace(string(runes[len(runes)-last:]))
}

func buildCanonicalContext(components map[setup.ComponentKey]setup.Component, exclude setup.ComponentKey) map[string]json.RawMessage {
	canon := make(map[string]json.RawMessage, len(components))
	remaining := canonicalContextChars
	for _, key := range setup.AllKeys {
		component, ok := components[key]
		if !ok || key == exclude || remaining < 200 {
			continue
		}
		var value any
		if json.Unmarshal(component.Payload, &value) != nil {
			continue
		}
		compact := compactCanonicalValue(value, 520, 7, 0)
		raw, err := json.Marshal(compact)
		if err != nil {
			continue
		}
		if len(raw) > remaining {
			compact = compactCanonicalValue(value, 220, 4, 0)
			raw, err = json.Marshal(compact)
		}
		if err != nil || len(raw) > remaining {
			continue
		}
		canon[string(key)] = raw
		remaining -= len(raw)
	}
	return canon
}

func compactCanonicalValue(value any, stringLimit, arrayLimit, depth int) any {
	if depth > 5 {
		return nil
	}
	switch typed := value.(type) {
	case string:
		return truncateRunes(strings.TrimSpace(typed), stringLimit)
	case []any:
		limit := min(len(typed), arrayLimit)
		out := make([]any, 0, limit)
		for _, item := range typed[:limit] {
			out = append(out, compactCanonicalValue(item, stringLimit, arrayLimit, depth+1))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = compactCanonicalValue(item, stringLimit, arrayLimit, depth+1)
		}
		return out
	default:
		return value
	}
}

func truncateRunes(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

func cloneRawMap(input map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(input)+1)
	for key, value := range input {
		out[key] = value
	}
	return out
}

func questsHaveFields(quests []any, requireStages bool) bool {
	for _, rawQuest := range quests {
		quest, ok := rawQuest.(map[string]any)
		if !ok {
			return false
		}
		for _, field := range []string{"title", "description", "successCriteria"} {
			value, ok := quest[field].(string)
			if !ok || strings.TrimSpace(value) == "" {
				return false
			}
		}
		if !requireStages {
			continue
		}
		stages, ok := quest["stages"].([]any)
		if !ok || len(stages) < 2 || len(stages) > 4 {
			return false
		}
		for _, rawStage := range stages {
			stage, ok := rawStage.(map[string]any)
			if !ok {
				return false
			}
			for _, field := range []string{"kind", "title", "description", "successCriteria"} {
				value, ok := stage[field].(string)
				if !ok || strings.TrimSpace(value) == "" {
					return false
				}
			}
		}
	}
	return true
}

func mergeQuestPhases(outlineRaw, detailsRaw json.RawMessage) (json.RawMessage, error) {
	var outline, details map[string]any
	if json.Unmarshal(outlineRaw, &outline) != nil || json.Unmarshal(detailsRaw, &details) != nil {
		return nil, setup.ErrInvalidComponent
	}
	outlines, outlineOK := outline["quests"].([]any)
	detailed, detailOK := details["quests"].([]any)
	if !outlineOK || !detailOK || len(outlines) != len(detailed) {
		return nil, setup.ErrInvalidComponent
	}
	detailsByTitle := make(map[string]map[string]any, len(detailed))
	for _, item := range detailed {
		quest, ok := item.(map[string]any)
		if !ok {
			return nil, setup.ErrInvalidComponent
		}
		detailsByTitle[strings.ToLower(strings.TrimSpace(asString(quest["title"])))] = quest
	}
	merged := make([]any, 0, len(outlines))
	for index, item := range outlines {
		quest, ok := item.(map[string]any)
		if !ok {
			return nil, setup.ErrInvalidComponent
		}
		copyQuest := make(map[string]any, len(quest)+1)
		for key, value := range quest {
			if key != "stages" {
				copyQuest[key] = value
			}
		}
		detail := detailsByTitle[strings.ToLower(strings.TrimSpace(asString(quest["title"])))]
		if detail == nil && index < len(detailed) {
			detail, _ = detailed[index].(map[string]any)
		}
		if detail == nil {
			return nil, setup.ErrInvalidComponent
		}
		copyQuest["stages"] = detail["stages"]
		merged = append(merged, copyQuest)
	}
	return json.Marshal(map[string]any{"quests": merged})
}

func openingBlueprintIsValid(object map[string]any) bool {
	object = normalizedOpeningBlueprint(object)
	choices, choicesOK := object["choices"].([]any)
	beats, beatsOK := object["beats"].([]any)
	if !choicesOK || len(choices) != 4 || !beatsOK || len(beats) < 4 || len(beats) > 6 {
		return false
	}
	for _, field := range []string{"chapterTitle", "chapterGoal", "sceneGoal"} {
		if strings.TrimSpace(asString(object[field])) == "" {
			return false
		}
	}
	return true
}

func normalizedOpeningBlueprint(object map[string]any) map[string]any {
	copyObject := make(map[string]any, len(object)+1)
	for key, value := range object {
		copyObject[key] = value
	}
	if choices, ok := copyObject["choices"].([]any); ok {
		copyObject["choices"] = normalizedStringList(choices, []string{"text", "label", "action", "title"})
	}
	beats, ok := copyObject["beats"].([]any)
	if !ok {
		beats, _ = copyObject["paragraphBeats"].([]any)
	}
	if len(beats) == 0 {
		if text, ok := copyObject["text"].(string); ok {
			paragraphs := nonEmptyParagraphs(strings.ReplaceAll(text, "\r\n", "\n"))
			beats = make([]any, 0, len(paragraphs))
			for _, paragraph := range paragraphs {
				beats = append(beats, paragraph)
			}
		}
	}
	copyObject["beats"] = normalizedStringList(beats, []string{"event", "summary", "text", "beat"})
	return copyObject
}

func normalizedStringList(values []any, fields []string) []any {
	out := make([]any, 0, len(values))
	for _, raw := range values {
		switch value := raw.(type) {
		case string:
			if text := strings.TrimSpace(value); text != "" {
				out = append(out, text)
			}
		case map[string]any:
			for _, field := range fields {
				if text := strings.TrimSpace(asString(value[field])); text != "" {
					out = append(out, text)
					break
				}
			}
		}
	}
	return out
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

func setupComponentTokenLimit(key setup.ComponentKey) int {
	switch key {
	case setup.StoryBible, setup.Player:
		return 1024
	case setup.World, setup.InitialCast, setup.VisualBible:
		return 1600
	case setup.WorldRules:
		return 3600
	case setup.InitialQuests:
		return 2400
	case setup.OpeningSituation:
		return 2400
	default:
		return 2048
	}
}

func setupComponentOutputConstraints(key setup.ComponentKey) string {
	base := "Return only the requested component in one complete JSON object. Keep non-opening fields concise and never repeat phrases, list items, tags, or sentences. Finish the JSON well before the token limit."
	switch key {
	case setup.VisualBible:
		return base + " Keep style, palette and cinematography to 1-3 sentences each; use at most 8 continuity rules. negativePromptEn must be exactly: low quality, blurry, distorted anatomy, extra limbs, text, watermark, inconsistent character design. Do not change, extend, or repeat that exact value."
	case setup.InitialCast:
		return base + " Include only recurring characters needed at the beginning, normally 2-6 characters; keep every description to 1-3 sentences."
	case setup.World:
		return base + " Include only 2-6 locations needed near the beginning; keep summaries and descriptions to 1-3 sentences."
	case setup.WorldRules:
		return base + " Describe systems and rules as separate addressable objects. Include explicit sources, capabilities, costs, limits, failure modes and progression. Never use vague laws such as magic works mysteriously."
	case setup.InitialQuests:
		return base + " Include 1-3 quest lines with questType main or side and 2-4 concise stages per quest; reserve main for the central plot and do not duplicate quests or stages."
	case setup.OpeningSituation:
		return "Return only opening_situation in one complete JSON object. The text MUST contain 4-6 distinct Russian paragraphs separated by two newline characters (a blank line), roughly 350-550 Russian words total, and exactly four short choices. Every paragraph must advance the scene. Never repeat or paraphrase paragraphs, sentences, dialogue, observations or conclusions. Finish the JSON within 2400 tokens."
	default:
		return base
	}
}

func extractSetupComponent(output json.RawMessage, key setup.ComponentKey) (json.RawMessage, error) {
	if !json.Valid(output) || string(output) == "null" {
		return nil, ErrIncompleteDraft
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(output, &payload); err != nil {
		// Be liberal for direct array responses in tests or providers that do not
		// enforce json_object. Component validation below still owns the shape.
		return output, nil
	}
	if raw, ok := payload[string(key)]; ok && json.Valid(raw) && string(raw) != "null" {
		return raw, nil
	}
	for _, known := range setup.AllKeys {
		if _, containsDifferentComponent := payload[string(known)]; containsDifferentComponent {
			return nil, fmt.Errorf("%w: missing %s", ErrIncompleteDraft, key)
		}
	}
	// When only one section is requested some local models omit the outer
	// component key and return the component object directly.
	return output, nil
}

func normalizeGeneratedComponent(key setup.ComponentKey, raw json.RawMessage) (json.RawMessage, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if key == setup.InitialCast {
		if characters, ok := value.([]any); ok {
			value = map[string]any{"characters": characters}
		}
	}
	if key == setup.OpeningSituation {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, setup.ErrInvalidComponent
		}
		if choices, exists := object["choices"].([]any); exists {
			normalized := make([]any, 0, len(choices))
			for _, rawChoice := range choices {
				switch choice := rawChoice.(type) {
				case string:
					if text := strings.TrimSpace(choice); text != "" {
						normalized = append(normalized, text)
					}
				case map[string]any:
					for _, field := range []string{"text", "label", "action", "title"} {
						if text, ok := choice[field].(string); ok && strings.TrimSpace(text) != "" {
							normalized = append(normalized, strings.TrimSpace(text))
							break
						}
					}
				}
			}
			object["choices"] = normalized
		}
		text, textOK := object["text"].(string)
		choices, choicesOK := object["choices"].([]any)
		if !textOK || !choicesOK || len(choices) != 4 {
			return nil, setup.ErrInvalidComponent
		}
		text = strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n")
		paragraphs := nonEmptyParagraphs(text)
		if len(paragraphs) < 4 {
			// Some local models use a single newline where the contract asks for a
			// blank one. Preserve those boundaries instead of rejecting good prose.
			lines := make([]string, 0, 9)
			for _, line := range strings.Split(text, "\n") {
				if line = strings.TrimSpace(line); line != "" {
					lines = append(lines, line)
				}
			}
			if len(lines) >= 4 {
				paragraphs = lines
			}
		}
		if !openingParagraphsAreValid(paragraphs) {
			return nil, setup.ErrInvalidComponent
		}
		object["text"] = strings.Join(paragraphs, "\n\n")
		value = object
	}
	if key == setup.InitialQuests {
		if quests, ok := value.([]any); ok {
			value = map[string]any{"quests": quests}
		}
		object, ok := value.(map[string]any)
		if !ok {
			return nil, setup.ErrInvalidComponent
		}
		quests, ok := object["quests"].([]any)
		if !ok || len(quests) == 0 {
			return nil, setup.ErrInvalidComponent
		}
		for _, rawQuest := range quests {
			quest, ok := rawQuest.(map[string]any)
			if !ok {
				return nil, setup.ErrInvalidComponent
			}
			quest["successCriteria"] = normalizeCriterion(quest["successCriteria"])
			stages, ok := quest["stages"].([]any)
			if !ok {
				return nil, setup.ErrInvalidComponent
			}
			for _, rawStage := range stages {
				stage, ok := rawStage.(map[string]any)
				if !ok {
					return nil, setup.ErrInvalidComponent
				}
				stage["successCriteria"] = normalizeCriterion(stage["successCriteria"])
			}
		}
		value = object
	}
	if key == setup.WorldRules {
		var err error
		value, err = normalizeWorldRulesValue(value)
		if err != nil {
			return nil, err
		}
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, setup.ErrInvalidComponent
	}
	out, err := json.Marshal(value)
	return json.RawMessage(out), err
}

func normalizeWorldRulesValue(value any) (any, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, setup.ErrInvalidComponent
	}
	systems, ok := object["systems"].([]any)
	if !ok || len(systems) < 1 || len(systems) > 6 {
		return nil, setup.ErrInvalidComponent
	}
	systemIDs := map[string]bool{}
	for index, rawSystem := range systems {
		system, ok := rawSystem.(map[string]any)
		if !ok {
			return nil, setup.ErrInvalidComponent
		}
		name := strings.TrimSpace(fmt.Sprint(system["name"]))
		if name == "" || name == "<nil>" {
			return nil, setup.ErrInvalidComponent
		}
		idValue := normalizedRuleID(fmt.Sprint(system["id"]), fmt.Sprintf("system-%d", index+1), false)
		if systemIDs[idValue] {
			return nil, setup.ErrInvalidComponent
		}
		systemIDs[idValue] = true
		system["id"], system["name"] = idValue, name
		kind := strings.ToLower(strings.TrimSpace(fmt.Sprint(system["kind"])))
		if !oneOf(kind, "world", "magic", "neural", "technology", "divine", "mental", "social", "other") {
			kind = "other"
		}
		system["kind"] = kind
		if _, ok := system["description"].(string); !ok {
			system["description"] = ""
		}
		resources, ok := system["resources"].([]any)
		if !ok {
			resources = []any{}
		}
		if len(resources) > 8 {
			resources = resources[:8]
		}
		for resourceIndex, rawResource := range resources {
			resource, ok := rawResource.(map[string]any)
			if !ok || strings.TrimSpace(fmt.Sprint(resource["name"])) == "" {
				return nil, setup.ErrInvalidComponent
			}
			resource["id"] = normalizedRuleID(fmt.Sprint(resource["id"]), fmt.Sprintf("resource-%d", resourceIndex+1), false)
			scope := strings.ToLower(strings.TrimSpace(fmt.Sprint(resource["ownerScope"])))
			if scope != "hero" && scope != "world" {
				scope = "world"
			}
			resource["ownerScope"] = scope
		}
		system["resources"] = resources
	}
	rules, ok := object["rules"].([]any)
	if !ok || len(rules) < 1 || len(rules) > 24 {
		return nil, setup.ErrInvalidComponent
	}
	ruleIDs := map[string]bool{}
	for index, rawRule := range rules {
		rule, ok := rawRule.(map[string]any)
		if !ok {
			return nil, setup.ErrInvalidComponent
		}
		idValue := normalizedRuleID(fmt.Sprint(rule["id"]), fmt.Sprintf("RULE-%03d", index+1), true)
		if ruleIDs[idValue] {
			return nil, setup.ErrInvalidComponent
		}
		ruleIDs[idValue] = true
		rule["id"] = idValue
		systemID := normalizedRuleID(fmt.Sprint(rule["systemId"]), "", false)
		if !systemIDs[systemID] {
			return nil, setup.ErrInvalidComponent
		}
		rule["systemId"] = systemID
		for _, field := range []string{"title", "statement"} {
			text := strings.TrimSpace(fmt.Sprint(rule[field]))
			if text == "" || text == "<nil>" {
				return nil, setup.ErrInvalidComponent
			}
			rule[field] = text
		}
		category := strings.ToLower(strings.TrimSpace(fmt.Sprint(rule["category"])))
		if !oneOf(category, "axiom", "law", "mechanism", "limit", "cost", "progression", "exception", "social", "terminology") {
			category = "law"
		}
		rule["category"] = category
		severity := strings.ToLower(strings.TrimSpace(fmt.Sprint(rule["severity"])))
		if !oneOf(severity, "hard", "soft", "mystery", "belief") {
			severity = "soft"
		}
		rule["severity"] = severity
		visibility := strings.ToLower(strings.TrimSpace(fmt.Sprint(rule["visibility"])))
		if !oneOf(visibility, "canon_only", "known_to_hero", "public", "hidden") {
			visibility = "canon_only"
		}
		rule["visibility"] = visibility
		status := strings.ToLower(strings.TrimSpace(fmt.Sprint(rule["status"])))
		if !oneOf(status, "established", "pending", "superseded", "archived") {
			status = "established"
		}
		rule["status"] = status
		for _, field := range []string{"preconditions", "costs", "forbiddenResults", "exceptions", "tags"} {
			if _, ok := rule[field].([]any); !ok {
				rule[field] = []any{}
			}
		}
	}
	glossary, ok := object["glossary"].([]any)
	if !ok {
		glossary = []any{}
	}
	if len(glossary) > 30 {
		glossary = glossary[:30]
	}
	object["systems"], object["rules"], object["glossary"] = systems, rules, glossary
	return object, nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func normalizedRuleID(value, fallback string, upper bool) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "<nil>" {
		value = fallback
	}
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if r == '-' || r == '_' || r == ' ' {
			if b.Len() > 0 {
				b.WriteByte('-')
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = fallback
	}
	if upper {
		return strings.ToUpper(out)
	}
	return strings.ToLower(out)
}

func nonEmptyParagraphs(text string) []string {
	parts := strings.Split(text, "\n\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func openingProseIsValid(text string) bool {
	paragraphs := nonEmptyParagraphs(text)
	if len(paragraphs) < 4 {
		paragraphs = nonEmptyLines(text)
	}
	return openingParagraphsAreValid(paragraphs)
}

func openingParagraphsAreValid(paragraphs []string) bool {
	if len(paragraphs) < 4 || len(paragraphs) > 6 {
		return false
	}
	for i := range paragraphs {
		for j := 0; j < i; j++ {
			if paragraphOverlap(paragraphs[i], paragraphs[j]) >= 0.18 {
				return false
			}
		}
	}
	return true
}

func paragraphOverlap(left, right string) float64 {
	leftShingles := wordShingles(normalizedTextWords(left), 3)
	rightShingles := wordShingles(normalizedTextWords(right), 3)
	if len(leftShingles) == 0 || len(rightShingles) == 0 {
		return 0
	}
	matched := 0
	for shingle := range leftShingles {
		if _, exists := rightShingles[shingle]; exists {
			matched++
		}
	}
	denominator := min(len(leftShingles), len(rightShingles))
	return float64(matched) / float64(denominator)
}

func normalizedTextWords(text string) []string {
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, text)
	return strings.Fields(normalized)
}

func wordShingles(words []string, size int) map[string]struct{} {
	out := map[string]struct{}{}
	for index := 0; size > 0 && index+size <= len(words); index++ {
		out[strings.Join(words[index:index+size], " ")] = struct{}{}
	}
	return out
}

func nonEmptyLines(text string) []string {
	parts := strings.Split(text, "\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizeCriterion(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "; ")
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
func (s Service) Assist(ctx context.Context, c AssistCommand) (AssistResult, error) {
	instruction := strings.TrimSpace(c.Instruction)
	if instruction == "" || len([]rune(instruction)) > 6000 {
		return AssistResult{}, ErrInvalidAssistInstruction
	}
	st, err := s.Repo.GetStory(ctx, c.StoryID)
	if err != nil {
		return AssistResult{}, err
	}
	components, err := s.Repo.ListComponents(ctx, c.StoryID)
	if err != nil {
		return AssistResult{}, err
	}
	byKey := make(map[setup.ComponentKey]setup.Component, len(components))
	for _, component := range components {
		byKey[component.Key] = component
	}

	requested := make(map[setup.ComponentKey]bool)
	if len(c.Keys) == 0 {
		for _, key := range setup.AllKeys {
			requested[key] = true
		}
	} else {
		for _, key := range c.Keys {
			if !key.Valid() {
				return AssistResult{}, setup.ErrInvalidComponent
			}
			requested[key] = true
		}
	}
	for key := range requested {
		component, ok := byKey[key]
		if !ok || component.Locked {
			delete(requested, key)
		}
	}
	if len(requested) == 0 {
		return AssistResult{}, ErrNoEditableComponents
	}

	// Effective setup starts from canonical payloads and overlays any valid
	// unsaved browser drafts. The drafts are deliberately never persisted here.
	effective := make(map[setup.ComponentKey]json.RawMessage, len(byKey))
	locked := make([]string, 0)
	for _, key := range setup.AllKeys {
		component, ok := byKey[key]
		if !ok {
			continue
		}
		effective[key] = append(json.RawMessage(nil), component.Payload...)
		if component.Locked {
			locked = append(locked, string(key))
		}
	}
	for key, raw := range c.Drafts {
		if !key.Valid() {
			return AssistResult{}, setup.ErrInvalidComponent
		}
		if _, ok := byKey[key]; !ok {
			continue
		}
		if !isJSONObject(raw) {
			return AssistResult{}, setup.ErrInvalidComponent
		}
		effective[key] = append(json.RawMessage(nil), raw...)
	}

	setupInput := make(map[string]json.RawMessage, len(effective))
	for _, key := range setup.AllKeys {
		if raw, ok := effective[key]; ok {
			setupInput[string(key)] = raw
		}
	}
	gidRaw, err := id.New()
	if err != nil {
		return AssistResult{}, err
	}
	gid := event.GenerationID(gidRaw)
	llm := s.LLM
	if s.LLMFactory != nil {
		llm, err = s.LLMFactory(ctx)
		if err != nil {
			return AssistResult{}, err
		}
	}
	if llm == nil {
		return AssistResult{}, aiport.ErrUnavailable
	}
	ident := llm.Identity()
	if err = s.Repo.CreateSetupGeneration(ctx, gid, c.StoryID, ident.Provider, ident.Model, ident.Profile); err != nil {
		return AssistResult{}, err
	}
	fail := func(message string) {
		_ = s.Repo.FinishSetupGeneration(ctx, gid, "failed", &message)
	}

	proposal := assistProposal{Operations: []AssistOperation{}}
	summaries := make([]string, 0, len(requested))
	type componentAssistResult struct {
		key      setup.ComponentKey
		proposal assistProposal
		err      error
	}
	byResult := make(map[setup.ComponentKey]componentAssistResult, len(requested))
	if llmpolicy.SupportsParallelRequests(ident) && len(requested) > 1 {
		results := make(chan componentAssistResult, len(requested))
		limit := make(chan struct{}, 3)
		var wait sync.WaitGroup
		for _, key := range setup.AllKeys {
			if !requested[key] {
				continue
			}
			wait.Add(1)
			go func(componentKey setup.ComponentKey) {
				defer wait.Done()
				limit <- struct{}{}
				defer func() { <-limit }()
				componentProposal, proposalErr := generateComponentAssist(ctx, llm, st, componentKey, strings.TrimSpace(c.Action), c.Target, instruction, locked, setupInput, effective[componentKey])
				results <- componentAssistResult{key: componentKey, proposal: componentProposal, err: proposalErr}
			}(key)
		}
		wait.Wait()
		close(results)
		for value := range results {
			byResult[value.key] = value
		}
	} else {
		for _, key := range setup.AllKeys {
			if !requested[key] {
				continue
			}
			componentProposal, proposalErr := generateComponentAssist(ctx, llm, st, key, strings.TrimSpace(c.Action), c.Target, instruction, locked, setupInput, effective[key])
			byResult[key] = componentAssistResult{key: key, proposal: componentProposal, err: proposalErr}
			if proposalErr != nil {
				break
			}
		}
	}
	for _, key := range setup.AllKeys {
		if !requested[key] {
			continue
		}
		value := byResult[key]
		if value.err != nil {
			fail(fmt.Sprintf("invalid %s assistant proposal after retry: %v", key, value.err))
			return AssistResult{}, ErrInvalidAssistProposal
		}
		proposal.Operations = append(proposal.Operations, value.proposal.Operations...)
		if summary := strings.TrimSpace(value.proposal.Summary); summary != "" {
			summaries = append(summaries, summary)
		}
	}
	proposal.Summary = strings.Join(summaries, " · ")

	working := make(map[setup.ComponentKey]json.RawMessage, len(effective))
	for key, raw := range effective {
		working[key] = append(json.RawMessage(nil), raw...)
	}
	for _, operation := range proposal.Operations {
		if !operation.Component.Valid() || !requested[operation.Component] {
			fail("proposal modified a locked, unrequested, or invalid component")
			return AssistResult{}, ErrInvalidAssistProposal
		}
		after, applyErr := applyAssistOperation(working[operation.Component], operation)
		if applyErr != nil {
			fail("invalid or ambiguous addressable operation")
			return AssistResult{}, ErrInvalidAssistProposal
		}
		working[operation.Component] = after
	}
	if requested[setup.WorldRules] {
		if raw, ok := working[setup.WorldRules]; ok {
			normalized, normalizeErr := normalizeGeneratedComponent(setup.WorldRules, raw)
			if normalizeErr != nil {
				fail("world rule operations produced an invalid rulebook")
				return AssistResult{}, ErrInvalidAssistProposal
			}
			working[setup.WorldRules] = normalized
		}
	}

	changes := make([]AssistChange, 0, len(proposal.Operations))
	for _, key := range setup.AllKeys {
		after, ok := working[key]
		if !ok || !requested[key] {
			continue
		}
		before := effective[key]
		paths := changedJSONPaths(before, after)
		if len(paths) == 0 {
			continue
		}
		changes = append(changes, AssistChange{
			Key:          key,
			Before:       append(json.RawMessage(nil), before...),
			After:        append(json.RawMessage(nil), after...),
			ChangedPaths: paths,
			Operations:   operationsForComponent(proposal.Operations, key),
		})
	}
	if err = s.Repo.FinishSetupGeneration(ctx, gid, "completed", nil); err != nil {
		return AssistResult{}, err
	}
	summary := strings.TrimSpace(proposal.Summary)
	if summary == "" {
		if len(changes) == 0 {
			summary = "AI не нашёл изменений, которые стоит применить к выбранному setup."
		} else {
			summary = "AI подготовил предложение по изменению setup."
		}
	}
	return AssistResult{GenerationID: gid, Summary: summary, Changes: changes}, nil
}

func generateComponentAssist(ctx context.Context, llm aiport.StoryLLM, st story.Story, key setup.ComponentKey, action string, target AssistTarget, instruction string, locked []string, setupInput map[string]json.RawMessage, current json.RawMessage) (assistProposal, error) {
	componentWorking := append(json.RawMessage(nil), current...)
	combined := assistProposal{Operations: []AssistOperation{}}
	summaries := []string{}
	for _, plan := range assistPlans(key, action, target, instruction, componentWorking) {
		planSetup := make(map[string]json.RawMessage, len(setupInput))
		for setupKey, payload := range setupInput {
			planSetup[setupKey] = payload
		}
		planSetup[string(key)] = componentWorking
		componentProposal, err := generateAssistProposal(ctx, llm, st, plan.Instruction, plan.Action, plan.Target, key, locked, planSetup, componentWorking)
		if err != nil {
			return assistProposal{}, err
		}
		for _, operation := range componentProposal.Operations {
			componentWorking, err = applyAssistOperation(componentWorking, operation)
			if err != nil {
				return assistProposal{}, err
			}
		}
		combined.Operations = append(combined.Operations, componentProposal.Operations...)
		if summary := strings.TrimSpace(componentProposal.Summary); summary != "" {
			summaries = append(summaries, summary)
		}
	}
	combined.Summary = strings.Join(summaries, " · ")
	return combined, nil
}

const setupAssistAttempts = 2

type assistRequestPlan struct {
	Instruction string
	Action      string
	Target      AssistTarget
}

func assistPlans(key setup.ComponentKey, action string, target AssistTarget, instruction string, current json.RawMessage) []assistRequestPlan {
	fieldPlan := func(paths ...string) []assistRequestPlan {
		plans := make([]assistRequestPlan, 0, len(paths))
		for _, path := range paths {
			plans = append(plans, assistRequestPlan{
				Instruction: instruction + " Измени только поле `" + path + "`; не возвращай и не переписывай другие поля.",
				Action:      "set_field",
				Target:      AssistTarget{Path: path},
			})
		}
		return plans
	}
	switch action {
	case "improve_player_profile":
		return fieldPlan("description")
	case "improve_player_motivation":
		return fieldPlan("description", "goals")
	case "improve_player_visual":
		return fieldPlan("visualAnchorEn")
	case "improve_visual_style":
		return fieldPlan("style", "palette", "cinematography")
	case "improve_visual_camera":
		return fieldPlan("cinematography")
	case "improve_opening_prose":
		return fieldPlan("text")
	case "improve_opening_choices":
		return fieldPlan("choices")
	case "improve_cast_roles", "improve_cast_visuals":
		var payload struct {
			Characters []map[string]any `json:"characters"`
		}
		if json.Unmarshal(current, &payload) != nil {
			return []assistRequestPlan{{Instruction: instruction, Action: "improve", Target: target}}
		}
		plans := make([]assistRequestPlan, 0, len(payload.Characters))
		for _, character := range payload.Characters {
			name := strings.TrimSpace(fmt.Sprint(character["name"]))
			if name == "" || name == "<nil>" {
				continue
			}
			focus := "role, personality и relationship"
			if action == "improve_cast_visuals" {
				focus = "только visualAnchorEn на английском"
			}
			plans = append(plans, assistRequestPlan{
				Instruction: instruction + " Для персонажа «" + name + "» измени " + focus + "; не меняй имя и остальных персонажей.",
				Action:      "update_item",
				Target:      AssistTarget{Path: "characters", MatchField: "name", MatchValue: name},
			})
		}
		if len(plans) > 0 {
			return plans
		}
	}
	return []assistRequestPlan{{Instruction: instruction, Action: action, Target: target}}
}

func generateAssistProposal(ctx context.Context, llm aiport.StoryLLM, st story.Story, instruction, action string, target AssistTarget, key setup.ComponentKey, locked []string, setupInput map[string]json.RawMessage, current json.RawMessage) (assistProposal, error) {
	input, err := json.Marshal(map[string]any{
		"title":               st.Title,
		"idea":                st.Description,
		"instruction":         instruction,
		"activeComponent":     string(key),
		"action":              action,
		"target":              target,
		"requestedComponents": []string{string(key)},
		"lockedComponents":    locked,
		"maxOperations":       assistOperationLimit(key, action),
		"outputConstraints":   assistOutputConstraints(key, action, target),
		"setup":               setupInput,
	})
	if err != nil {
		return assistProposal{}, err
	}

	var lastErr error
	for attempt := 0; attempt < setupAssistAttempts; attempt++ {
		response, generateErr := llm.Generate(ctx, aiport.StoryRequest{Role: "setup_editor", PromptVersion: "v1", Input: input, MaxTokens: assistTokenLimit(key, action, target)})
		if generateErr != nil {
			lastErr = generateErr
			continue
		}
		proposal, decodeErr := decodeAssistProposal(response.Output)
		if decodeErr != nil {
			lastErr = decodeErr
			continue
		}
		proposal.Operations = normalizeAssistOperations(proposal.Operations, key, action, target)
		if proposal.Operations == nil || len(proposal.Operations) > assistOperationLimit(key, action) {
			lastErr = ErrInvalidAssistProposal
			continue
		}

		working := append(json.RawMessage(nil), current...)
		valid := make([]AssistOperation, 0, len(proposal.Operations))
		for _, operation := range proposal.Operations {
			if !assistOperationAllowed(operation, key, action) {
				if isAddressableAction(action) {
					valid = nil
					break
				}
				continue
			}
			if operation.Operation == "set_field" {
				operation.Value = coerceSetFieldValue(working, operation.Path, operation.Value)
			}
			if operation.Operation == "add_item" && collectionAlreadyContains(working, operation.Path, operation.Value) {
				proposal.Summary = "Такой элемент уже есть в разделе; дубликат не добавлен."
				proposal.Operations = []AssistOperation{}
				return proposal, nil
			}
			after, applyErr := applyAssistOperation(working, operation)
			if applyErr != nil {
				if isAddressableAction(action) {
					valid = nil
					break
				}
				continue
			}
			working = after
			valid = append(valid, operation)
		}
		if isAddressableAction(action) && len(valid) != 1 {
			lastErr = ErrInvalidAssistProposal
			continue
		}
		if len(proposal.Operations) > 0 && len(valid) == 0 {
			lastErr = ErrInvalidAssistProposal
			continue
		}
		if len(valid) > 0 {
			if _, validationErr := normalizeGeneratedComponent(key, working); validationErr != nil {
				lastErr = validationErr
				continue
			}
		}
		proposal.Operations = valid
		return proposal, nil
	}
	if lastErr == nil {
		lastErr = ErrInvalidAssistProposal
	}
	return assistProposal{}, lastErr
}

func decodeAssistProposal(output json.RawMessage) (assistProposal, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(output, &envelope); err != nil || envelope == nil {
		return assistProposal{}, ErrInvalidAssistProposal
	}
	proposal := assistProposal{}
	_ = json.Unmarshal(envelope["summary"], &proposal.Summary)

	operationsRaw := envelope["operations"]
	if len(operationsRaw) == 0 {
		operationsRaw = envelope["changes"]
	}
	if len(operationsRaw) == 0 {
		operationsRaw = envelope["edits"]
	}
	if len(operationsRaw) == 0 {
		if _, directOperation := envelope["operation"]; directOperation {
			operationsRaw = output
		} else {
			return assistProposal{}, ErrInvalidAssistProposal
		}
	}
	if err := json.Unmarshal(operationsRaw, &proposal.Operations); err == nil {
		return proposal, nil
	}
	var single AssistOperation
	if err := json.Unmarshal(operationsRaw, &single); err != nil {
		return assistProposal{}, ErrInvalidAssistProposal
	}
	proposal.Operations = []AssistOperation{single}
	return proposal, nil
}

func normalizeAssistOperations(operations []AssistOperation, key setup.ComponentKey, action string, target AssistTarget) []AssistOperation {
	if operations == nil {
		return nil
	}
	if isAddressableAction(action) && len(operations) > 1 {
		selected := operations[0]
		for _, operation := range operations {
			if normalizeAssistOperationName(operation.Operation) == action && normalizeAssistPath(operation.Path, key) == target.Path {
				selected = operation
				break
			}
		}
		operations = []AssistOperation{selected}
	}
	for index := range operations {
		operation := &operations[index]
		operation.Component = key
		operation.Operation = normalizeAssistOperationName(operation.Operation)
		operation.Path = normalizeAssistPath(operation.Path, key)
		operation.ChildPath = normalizeAssistPath(operation.ChildPath, key)
		operation.MatchField = normalizeAssistPath(operation.MatchField, key)
		operation.ChildMatchField = normalizeAssistPath(operation.ChildMatchField, key)
	}
	if !normalizeRequestedOperation(operations, action, target, key) || !operationsMatchRequestedAction(operations, action, target) {
		return nil
	}
	if isAddressableAction(action) && len(operations) == 1 {
		operations[0].Value = unwrapAddressableValue(operations[0].Value, operations[0].Path, operations[0].Operation)
	}
	return operations
}

func normalizeAssistOperationName(operation string) string {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "set", "replace_field", "update_field":
		return "set_field"
	case "delete_field":
		return "remove_field"
	case "append_item", "create_item":
		return "add_item"
	case "edit_item", "replace_item":
		return "update_item"
	case "delete_item":
		return "remove_item"
	default:
		return strings.ToLower(strings.TrimSpace(operation))
	}
}

func normalizeAssistPath(path string, key setup.ComponentKey) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimPrefix(path, ".")
	path = strings.TrimPrefix(path, "/")
	for _, prefix := range []string{string(key) + ".", string(key) + "/"} {
		path = strings.TrimPrefix(path, prefix)
	}
	return strings.TrimSpace(path)
}

func unwrapAddressableValue(raw json.RawMessage, path, operation string) json.RawMessage {
	if !isJSONObject(raw) {
		return raw
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return raw
	}
	wrapped, ok := object[path]
	if !ok {
		if operation == "set_field" && path == "style" {
			wrapped, ok = object["description"]
		}
		if operation == "set_field" && !ok && len(object) == 1 {
			for _, onlyValue := range object {
				wrapped, ok = onlyValue, true
			}
		}
		if !ok {
			return raw
		}
	}
	var items []json.RawMessage
	if operation != "set_field" && json.Unmarshal(wrapped, &items) == nil && len(items) == 1 {
		return items[0]
	}
	return wrapped
}

func isAddressableAction(action string) bool {
	switch action {
	case "set_field", "remove_field", "add_item", "update_item", "remove_item", "add_child_item", "update_child_item", "remove_child_item":
		return true
	default:
		return false
	}
}

func assistOperationAllowed(operation AssistOperation, key setup.ComponentKey, requestedAction string) bool {
	if operation.Component != key || !safeTopLevelPath(operation.Path) {
		return false
	}
	if key == setup.Player && operation.Operation == "set_field" && !validPlayerAssistValue(operation.Path, operation.Value) {
		return false
	}
	switch operation.Operation {
	case "set_field", "remove_field":
		return requestedAction == operation.Operation || !assistCollectionPath(key, operation.Path)
	case "add_item", "update_item", "remove_item":
		return assistCollectionPath(key, operation.Path)
	case "add_child_item", "update_child_item", "remove_child_item":
		return (key == setup.InitialQuests && operation.Path == "quests" && operation.ChildPath == "stages") ||
			(key == setup.WorldRules && operation.Path == "systems" && operation.ChildPath == "resources")
	default:
		return false
	}
}

func validPlayerAssistValue(path string, raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	switch path {
	case "name", "description", "visualAnchorEn":
		text, ok := value.(string)
		return ok && strings.TrimSpace(text) != ""
	case "age":
		age, ok := value.(float64)
		return ok && age >= 18 && age <= 150 && age == float64(int(age))
	case "goals":
		goals, ok := value.([]any)
		if !ok || len(goals) < 1 || len(goals) > 5 {
			return false
		}
		seen := map[string]bool{}
		for _, rawGoal := range goals {
			goal, ok := rawGoal.(string)
			goal = strings.TrimSpace(goal)
			key := strings.ToLower(goal)
			if !ok || goal == "" || seen[key] {
				return false
			}
			seen[key] = true
		}
	}
	return true
}

func assistCollectionPath(key setup.ComponentKey, path string) bool {
	collections := map[setup.ComponentKey]map[string]bool{
		setup.StoryBible:       {"themes": true, "narrativeRules": true},
		setup.Player:           {"goals": true},
		setup.World:            {"locations": true},
		setup.WorldRules:       {"systems": true, "rules": true, "glossary": true},
		setup.InitialCast:      {"characters": true},
		setup.VisualBible:      {"continuityRules": true},
		setup.InitialQuests:    {"quests": true},
		setup.OpeningSituation: {"choices": true},
	}
	return collections[key][path]
}

func collectionAlreadyContains(targetRaw json.RawMessage, path string, valueRaw json.RawMessage) bool {
	var target map[string]any
	var candidate any
	if json.Unmarshal(targetRaw, &target) != nil || json.Unmarshal(valueRaw, &candidate) != nil {
		return false
	}
	items, ok := target[path].([]any)
	if !ok {
		return false
	}
	candidateObject, isObject := candidate.(map[string]any)
	if !isObject {
		candidateText := strings.TrimSpace(strings.ToLower(fmt.Sprint(candidate)))
		for _, item := range items {
			if strings.TrimSpace(strings.ToLower(fmt.Sprint(item))) == candidateText {
				return true
			}
		}
		return false
	}
	for _, identityField := range []string{"id", "name", "title"} {
		expected := strings.TrimSpace(strings.ToLower(fmt.Sprint(candidateObject[identityField])))
		if expected == "" || expected == "<nil>" {
			continue
		}
		for _, item := range items {
			object, ok := item.(map[string]any)
			if ok && strings.TrimSpace(strings.ToLower(fmt.Sprint(object[identityField]))) == expected {
				return true
			}
		}
		return false
	}
	return false
}

func coerceSetFieldValue(targetRaw json.RawMessage, path string, proposedRaw json.RawMessage) json.RawMessage {
	var target map[string]any
	var proposed any
	if json.Unmarshal(targetRaw, &target) != nil || json.Unmarshal(proposedRaw, &proposed) != nil {
		return proposedRaw
	}
	if _, wantsString := target[path].(string); wantsString {
		switch value := proposed.(type) {
		case string:
			return proposedRaw
		case map[string]any:
			for _, candidate := range []string{path, "description", "text", "composition", "style", "palette"} {
				if text, ok := value[candidate].(string); ok && strings.TrimSpace(text) != "" {
					encoded, _ := json.Marshal(strings.TrimSpace(text))
					return encoded
				}
			}
		}
	}
	return proposedRaw
}

func assistOperationLimit(key setup.ComponentKey, action string) int {
	if isAddressableAction(action) {
		return 1
	}
	switch key {
	case setup.InitialCast:
		return 8
	case setup.OpeningSituation:
		return 5
	default:
		return 6
	}
}

func assistTokenLimit(key setup.ComponentKey, action string, target AssistTarget) int {
	if action == "set_field" {
		if key == setup.OpeningSituation && target.Path == "text" {
			return 4096
		}
		return 1200
	}
	switch key {
	case setup.OpeningSituation:
		return 4096
	case setup.InitialCast, setup.InitialQuests, setup.WorldRules:
		return 2600
	default:
		return 2000
	}
}

func assistOutputConstraints(key setup.ComponentKey, action string, target AssistTarget) string {
	base := fmt.Sprintf("Return a complete JSON object with summary and at most %d small operations for only %s. Use only top-level paths without component prefixes or JSON pointers. Never repeat text or return a whole component.", assistOperationLimit(key, action), key)
	if isAddressableAction(action) {
		detail := " Return exactly one operation matching action and target. Put only the new or changed item data in value."
		if action == "set_field" && target.Path == "choices" {
			detail += " The value must be one JSON array containing exactly four strings."
		} else if action == "set_field" && key == setup.Player && target.Path == "goals" {
			detail += " The value must be one JSON array containing 1-5 distinct concise goals in the setup language."
		} else if action == "set_field" && key == setup.Player && target.Path == "age" {
			detail += " The value must be one JSON integer from 18 to 150."
		} else if action == "set_field" {
			detail += " The value must be one JSON string for target.path, never an object."
		}
		return base + detail
	}
	switch key {
	case setup.VisualBible:
		return base + " Keep style fields concise. Never expand negativePromptEn; if changing it, use only unique short English tags and at most 250 characters."
	case setup.OpeningSituation:
		return base + " Change choices separately from text. Rewrite text only when the instruction explicitly concerns prose; preserve 4-6 paragraph boundaries and exactly four choices."
	case setup.InitialCast:
		return base + " Use update_item on characters rather than replacing characters. Preserve names unless explicitly asked otherwise."
	case setup.Player:
		return base + " Preserve the protagonist's name, age and established facts unless the instruction explicitly targets them. Keep description, goals and visualAnchorEn mutually consistent; visualAnchorEn must be reusable detailed English."
	case setup.WorldRules:
		return base + " Edit systems, rules and glossary as individual items. Preserve stable ids. A new hard law or exception must be explicit and testable; never silently replace existing laws."
	default:
		return base
	}
}

// For an entity chip, the browser selection is authoritative. Local models
// sometimes copy the right operation but invent an array index or JSON-pointer
// path. We keep only the model-authored value and bind it to the exact target
// selected by the user.
func normalizeRequestedOperation(operations []AssistOperation, action string, target AssistTarget, component setup.ComponentKey) bool {
	switch action {
	case "set_field", "remove_field", "add_item", "update_item", "remove_item", "add_child_item", "update_child_item", "remove_child_item":
		if len(operations) != 1 || !component.Valid() {
			return false
		}
		operations[0].Component = component
		operations[0].Operation = action
		operations[0].Path = target.Path
		operations[0].MatchField = target.MatchField
		operations[0].MatchValue = target.MatchValue
		operations[0].ChildPath = target.ChildPath
		operations[0].ChildMatchField = target.ChildMatchField
		operations[0].ChildMatchValue = target.ChildMatchValue
	}
	return true
}

func firstRequestedKey(requested map[setup.ComponentKey]bool) string {
	for _, key := range setup.AllKeys {
		if requested[key] {
			return string(key)
		}
	}
	return ""
}

func operationsForComponent(operations []AssistOperation, key setup.ComponentKey) []AssistOperation {
	out := make([]AssistOperation, 0)
	for _, operation := range operations {
		if operation.Component == key {
			out = append(out, operation)
		}
	}
	return out
}

func operationsMatchRequestedAction(operations []AssistOperation, action string, target AssistTarget) bool {
	switch action {
	case "", "custom", "improve", "review":
		return true
	case "set_field", "remove_field", "add_item", "update_item", "remove_item", "add_child_item", "update_child_item", "remove_child_item":
		if len(operations) != 1 {
			return false
		}
		op := operations[0]
		return op.Operation == action && op.Path == target.Path && op.MatchField == target.MatchField && op.MatchValue == target.MatchValue && op.ChildPath == target.ChildPath && op.ChildMatchField == target.ChildMatchField && op.ChildMatchValue == target.ChildMatchValue
	default:
		return false
	}
}

func applyAssistOperation(targetRaw json.RawMessage, operation AssistOperation) (json.RawMessage, error) {
	if !isJSONObject(targetRaw) || !safeTopLevelPath(operation.Path) {
		return nil, ErrInvalidAssistProposal
	}
	var target map[string]any
	if err := json.Unmarshal(targetRaw, &target); err != nil {
		return nil, err
	}
	switch operation.Operation {
	case "set_field":
		value, err := decodeJSONValue(operation.Value)
		if err != nil {
			return nil, err
		}
		target[operation.Path] = value
	case "remove_field":
		delete(target, operation.Path)
	case "add_item":
		items, err := collectionAt(target, operation.Path)
		if err != nil {
			return nil, err
		}
		value, err := decodeJSONValue(operation.Value)
		if err != nil {
			return nil, err
		}
		target[operation.Path] = append(items, value)
	case "update_item", "remove_item":
		items, err := collectionAt(target, operation.Path)
		if err != nil {
			return nil, err
		}
		index, err := uniqueMatch(items, operation.MatchField, operation.MatchValue)
		if err != nil {
			return nil, err
		}
		if operation.Operation == "remove_item" {
			target[operation.Path] = append(items[:index:index], items[index+1:]...)
		} else {
			updated, err := mergeItem(items[index], operation.Value)
			if err != nil {
				return nil, err
			}
			items[index] = updated
			target[operation.Path] = items
		}
	case "add_child_item", "update_child_item", "remove_child_item":
		if !safeTopLevelPath(operation.ChildPath) {
			return nil, ErrInvalidAssistProposal
		}
		parents, err := collectionAt(target, operation.Path)
		if err != nil {
			return nil, err
		}
		parentIndex, err := uniqueMatch(parents, operation.MatchField, operation.MatchValue)
		if err != nil {
			return nil, err
		}
		parent, ok := parents[parentIndex].(map[string]any)
		if !ok {
			return nil, ErrInvalidAssistProposal
		}
		children, err := collectionAt(parent, operation.ChildPath)
		if err != nil {
			return nil, err
		}
		if operation.Operation == "add_child_item" {
			value, decodeErr := decodeJSONValue(operation.Value)
			if decodeErr != nil {
				return nil, decodeErr
			}
			parent[operation.ChildPath] = append(children, value)
		} else {
			childIndex, matchErr := uniqueMatch(children, operation.ChildMatchField, operation.ChildMatchValue)
			if matchErr != nil {
				return nil, matchErr
			}
			if operation.Operation == "remove_child_item" {
				parent[operation.ChildPath] = append(children[:childIndex:childIndex], children[childIndex+1:]...)
			} else {
				updated, mergeErr := mergeItem(children[childIndex], operation.Value)
				if mergeErr != nil {
					return nil, mergeErr
				}
				children[childIndex] = updated
				parent[operation.ChildPath] = children
			}
		}
		parents[parentIndex] = parent
		target[operation.Path] = parents
	default:
		return nil, ErrInvalidAssistProposal
	}
	out, err := json.Marshal(target)
	return json.RawMessage(out), err
}

func safeTopLevelPath(path string) bool {
	path = strings.TrimSpace(path)
	return path != "" && path != "$" && !strings.ContainsAny(path, ".[]/\\")
}

func decodeJSONValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || !json.Valid(raw) || string(raw) == "null" {
		return nil, ErrInvalidAssistProposal
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func collectionAt(object map[string]any, path string) ([]any, error) {
	value, exists := object[path]
	if !exists {
		return []any{}, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, ErrInvalidAssistProposal
	}
	return items, nil
}

func uniqueMatch(items []any, field, expected string) (int, error) {
	match := -1
	for index, item := range items {
		actual := item
		if field != "" {
			object, ok := item.(map[string]any)
			if !ok {
				continue
			}
			actual = object[field]
		}
		if fmt.Sprint(actual) != expected {
			continue
		}
		if match >= 0 {
			return -1, ErrInvalidAssistProposal
		}
		match = index
	}
	if match < 0 {
		return -1, ErrInvalidAssistProposal
	}
	return match, nil
}

func mergeItem(current any, patchRaw json.RawMessage) (any, error) {
	currentObject, ok := current.(map[string]any)
	if !ok {
		return decodeJSONValue(patchRaw)
	}
	if !isJSONObject(patchRaw) {
		return nil, ErrInvalidAssistProposal
	}
	var patch map[string]any
	if err := json.Unmarshal(patchRaw, &patch); err != nil {
		return nil, err
	}
	return mergeObjects(currentObject, patch), nil
}

func isJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 || !json.Valid(raw) {
		return false
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return false
	}
	return object != nil
}

func applyObjectMergePatch(targetRaw, patchRaw json.RawMessage) (json.RawMessage, error) {
	var target map[string]any
	var patch map[string]any
	if err := json.Unmarshal(targetRaw, &target); err != nil || target == nil {
		return nil, ErrInvalidAssistProposal
	}
	if err := json.Unmarshal(patchRaw, &patch); err != nil || patch == nil {
		return nil, ErrInvalidAssistProposal
	}
	merged := mergeObjects(target, patch)
	out, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(out), nil
}

func mergeObjects(target, patch map[string]any) map[string]any {
	out := make(map[string]any, len(target)+len(patch))
	for key, value := range target {
		out[key] = value
	}
	for key, value := range patch {
		if value == nil {
			delete(out, key)
			continue
		}
		patchObject, isObject := value.(map[string]any)
		if !isObject {
			out[key] = value
			continue
		}
		current, _ := out[key].(map[string]any)
		if current == nil {
			current = map[string]any{}
		}
		out[key] = mergeObjects(current, patchObject)
	}
	return out
}

func changedJSONPaths(beforeRaw, afterRaw json.RawMessage) []string {
	var before, after any
	if json.Unmarshal(beforeRaw, &before) != nil || json.Unmarshal(afterRaw, &after) != nil {
		return nil
	}
	paths := make([]string, 0)
	collectChangedPaths("", before, after, &paths)
	sort.Strings(paths)
	if len(paths) > 64 {
		paths = paths[:64]
	}
	return paths
}

func collectChangedPaths(prefix string, before, after any, out *[]string) {
	if reflect.DeepEqual(before, after) {
		return
	}
	beforeObject, beforeOK := before.(map[string]any)
	afterObject, afterOK := after.(map[string]any)
	if beforeOK && afterOK {
		keySet := make(map[string]struct{}, len(beforeObject)+len(afterObject))
		for key := range beforeObject {
			keySet[key] = struct{}{}
		}
		for key := range afterObject {
			keySet[key] = struct{}{}
		}
		keys := make([]string, 0, len(keySet))
		for key := range keySet {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			left, leftOK := beforeObject[key]
			right, rightOK := afterObject[key]
			if !leftOK || !rightOK {
				*out = append(*out, path)
				continue
			}
			collectChangedPaths(path, left, right, out)
		}
		return
	}
	if prefix == "" {
		prefix = "$"
	}
	*out = append(*out, prefix)
}

func (s Service) Edit(ctx context.Context, c ManualEditCommand) (setup.Component, error) {
	if !c.Key.Valid() || !json.Valid(c.Payload) {
		return setup.Component{}, setup.ErrInvalidComponent
	}
	if c.Key == setup.WorldRules {
		normalized, err := normalizeGeneratedComponent(c.Key, c.Payload)
		if err != nil {
			return setup.Component{}, setup.ErrInvalidComponent
		}
		c.Payload = normalized
	}
	all, e := s.Repo.ListComponents(ctx, c.StoryID)
	if e != nil {
		return setup.Component{}, e
	}
	var found *setup.Component
	for i := range all {
		if all[i].Key == c.Key {
			found = &all[i]
			break
		}
	}
	if found == nil {
		return setup.Component{}, setup.ErrInvalidComponent
	}
	v := *found
	v.Revision++
	v.Source = "manual"
	v.Payload = append([]byte(nil), c.Payload...)
	v.Status = "ready"
	v.GenerationID = nil
	v.UpdatedAt = s.Now()
	if c.Locked != nil {
		v.Locked = *c.Locked
	}
	if e = s.Repo.SetComponent(ctx, v); e != nil {
		return setup.Component{}, e
	}
	return v, nil
}
func (s Service) SetLock(ctx context.Context, storyID story.ID, key setup.ComponentKey, locked bool) (setup.Component, error) {
	all, e := s.Repo.ListComponents(ctx, storyID)
	if e != nil {
		return setup.Component{}, e
	}
	for _, v := range all {
		if v.Key == key {
			v.Locked = locked
			v.UpdatedAt = s.Now()
			if e = s.Repo.SetComponent(ctx, v); e != nil {
				return setup.Component{}, e
			}
			return v, nil
		}
	}
	return setup.Component{}, setup.ErrInvalidComponent
}
func (s Service) Start(ctx context.Context, storyID story.ID) (StartResult, error) {
	all, e := s.Repo.ListComponents(ctx, storyID)
	if e != nil {
		return StartResult{}, e
	}
	m := map[setup.ComponentKey]setup.Component{}
	for _, v := range all {
		m[v.Key] = v
	}
	for _, k := range setup.AllKeys {
		v, ok := m[k]
		if !ok || v.Status != "ready" {
			return StartResult{}, setup.ErrNotReady
		}
	}
	tid, _ := id.New()
	tl, e := timeline.New(timeline.ID(tid), storyID, "Main")
	if e != nil {
		return StartResult{}, e
	}
	chapterID, _ := id.New()
	sceneID, _ := id.New()
	beatID, _ := id.New()
	var open struct {
		Text         string   `json:"text"`
		Choices      []string `json:"choices"`
		ChapterTitle string   `json:"chapterTitle"`
		ChapterGoal  string   `json:"chapterGoal"`
		SceneGoal    string   `json:"sceneGoal"`
	}
	if e = json.Unmarshal(m[setup.OpeningSituation].Payload, &open); e != nil || strings.TrimSpace(open.Text) == "" {
		return StartResult{}, setup.ErrNotReady
	}
	open.Choices = ensureFourOpeningChoices(open.Choices)
	quests, e := parseInitialQuests(m[setup.InitialQuests].Payload)
	if e != nil {
		return StartResult{}, setup.ErrNotReady
	}
	characters, locations, e := parseInitialWorld(m[setup.Player].Payload, m[setup.InitialCast].Payload, m[setup.World].Payload)
	if e != nil {
		return StartResult{}, setup.ErrNotReady
	}
	mat := setuprepo.StartMaterialization{ChapterID: chapterID, SceneID: sceneID, BeatID: beatID, ChapterTitle: open.ChapterTitle, ChapterGoal: open.ChapterGoal, SceneGoal: open.SceneGoal, OpeningText: open.Text, Choices: open.Choices, Quests: quests, Characters: characters, Locations: locations}
	if e = s.Repo.StartStory(ctx, storyID, tl, mat); e != nil {
		return StartResult{}, e
	}
	// The beat_committed database outbox schedules its illustration durably.
	// Starting a story no longer waits for the visual-prompt LLM request.
	return StartResult{Timeline: tl, SceneID: sceneID, BeatID: beatID}, nil
}

func parseInitialWorld(playerRaw, castRaw, worldRaw json.RawMessage) ([]setuprepo.InitialCharacter, []setuprepo.InitialLocation, error) {
	var player struct {
		Name, Description, VisualAnchorEn string
		Age                               int
	}
	if json.Unmarshal(playerRaw, &player) != nil || strings.TrimSpace(player.Name) == "" {
		return nil, nil, setup.ErrInvalidComponent
	}
	if player.Age < 1 || player.Age > 150 {
		player.Age = 18
	}
	characters := []setuprepo.InitialCharacter{{Kind: "player", Name: strings.TrimSpace(player.Name), Age: player.Age, Personality: strings.TrimSpace(player.Description), VisualAnchorEn: strings.TrimSpace(player.VisualAnchorEn)}}
	var cast struct {
		Characters []struct {
			Name, Role, Personality, Character, Relationship, RelationshipToHero, VisualAnchorEn string
			Age                                                                                  int
		} `json:"characters"`
	}
	if json.Unmarshal(castRaw, &cast) != nil {
		return nil, nil, setup.ErrInvalidComponent
	}
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(player.Name)): true}
	for _, raw := range cast.Characters {
		name := strings.TrimSpace(raw.Name)
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		age := raw.Age
		if age < 1 || age > 150 {
			age = 18
		}
		personality := raw.Personality
		if strings.TrimSpace(personality) == "" {
			personality = raw.Character
		}
		relation := raw.Relationship
		if strings.TrimSpace(relation) == "" {
			relation = raw.RelationshipToHero
		}
		characters = append(characters, setuprepo.InitialCharacter{Kind: "persistent_npc", Name: name, Age: age, Role: strings.TrimSpace(raw.Role), Personality: strings.TrimSpace(personality), Relationship: strings.TrimSpace(relation), VisualAnchorEn: strings.TrimSpace(raw.VisualAnchorEn)})
	}
	var world struct {
		Locations []setuprepo.InitialLocation `json:"locations"`
	}
	if json.Unmarshal(worldRaw, &world) != nil {
		return nil, nil, setup.ErrInvalidComponent
	}
	locations := make([]setuprepo.InitialLocation, 0, len(world.Locations))
	seenLocations := map[string]bool{}
	for _, location := range world.Locations {
		location.Name = strings.TrimSpace(location.Name)
		key := strings.ToLower(location.Name)
		if location.Name == "" || seenLocations[key] {
			continue
		}
		seenLocations[key] = true
		locations = append(locations, location)
	}
	return characters, locations, nil
}

func parseInitialQuests(raw json.RawMessage) ([]setuprepo.InitialQuest, error) {
	normalized, err := normalizeGeneratedComponent(setup.InitialQuests, raw)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Quests []setuprepo.InitialQuest `json:"quests"`
	}
	if err := json.Unmarshal(normalized, &payload); err != nil || len(payload.Quests) == 0 || len(payload.Quests) > 8 {
		return nil, setup.ErrInvalidComponent
	}
	questTitles := make(map[string]bool, len(payload.Quests))
	for questIndex := range payload.Quests {
		quest := &payload.Quests[questIndex]
		quest.Title = strings.TrimSpace(quest.Title)
		quest.QuestType = strings.ToLower(strings.TrimSpace(quest.QuestType))
		if quest.QuestType == "" {
			quest.QuestType = "main"
		}
		quest.Description = strings.TrimSpace(quest.Description)
		quest.SuccessCriteria = strings.TrimSpace(quest.SuccessCriteria)
		if quest.Title == "" || (quest.QuestType != "main" && quest.QuestType != "side") || len(quest.Stages) == 0 || len(quest.Stages) > 12 {
			return nil, setup.ErrInvalidComponent
		}
		key := strings.ToLower(quest.Title)
		if questTitles[key] {
			return nil, setup.ErrInvalidComponent
		}
		questTitles[key] = true
		if quest.SuccessCriteria == "" {
			quest.SuccessCriteria = quest.Title
		}
		stageTitles := make(map[string]bool, len(quest.Stages))
		for stageIndex := range quest.Stages {
			stage := &quest.Stages[stageIndex]
			stage.Kind = strings.TrimSpace(stage.Kind)
			stage.Title = strings.TrimSpace(stage.Title)
			stage.Description = strings.TrimSpace(stage.Description)
			stage.SuccessCriteria = strings.TrimSpace(stage.SuccessCriteria)
			if stage.Kind == "" {
				stage.Kind = "task"
			}
			if stage.Title == "" || (stage.Kind != "task" && stage.Kind != "event" && stage.Kind != "milestone") {
				return nil, setup.ErrInvalidComponent
			}
			stageKey := strings.ToLower(stage.Title)
			if stageTitles[stageKey] {
				return nil, setup.ErrInvalidComponent
			}
			stageTitles[stageKey] = true
			if stage.SuccessCriteria == "" {
				stage.SuccessCriteria = stage.Title
			}
		}
	}
	return payload.Quests, nil
}

func parseWorldRules(raw json.RawMessage) ([]setuprepo.InitialWorldSystem, []setuprepo.InitialWorldRule, error) {
	normalized, err := normalizeGeneratedComponent(setup.WorldRules, raw)
	if err != nil {
		return nil, nil, err
	}
	var payload struct {
		Systems []struct {
			ID, Name, Kind, Description string
			Resources                   []struct {
				ID, Name, Unit, OwnerScope string
				InitialValue               float64
				MinValue, MaxValue         *float64
			}
		} `json:"systems"`
		Rules []struct {
			ID, SystemID, Title, Category, Severity, Statement       string
			Preconditions, Costs, ForbiddenResults, Exceptions, Tags []string
			Visibility, Status, ExceptionOf                          string
		} `json:"rules"`
	}
	if json.Unmarshal(normalized, &payload) != nil {
		return nil, nil, setup.ErrInvalidComponent
	}
	systems := make([]setuprepo.InitialWorldSystem, 0, len(payload.Systems))
	for _, system := range payload.Systems {
		out := setuprepo.InitialWorldSystem{
			ID:          system.ID,
			Name:        system.Name,
			Kind:        system.Kind,
			Description: system.Description,
			Resources:   make([]setuprepo.InitialWorldResource, 0, len(system.Resources)),
		}
		for _, resource := range system.Resources {
			out.Resources = append(out.Resources, setuprepo.InitialWorldResource{ID: resource.ID, Name: resource.Name, Unit: resource.Unit, OwnerScope: resource.OwnerScope, InitialValue: resource.InitialValue, MinValue: resource.MinValue, MaxValue: resource.MaxValue})
		}
		systems = append(systems, out)
	}
	rules := make([]setuprepo.InitialWorldRule, 0, len(payload.Rules))
	for _, rule := range payload.Rules {
		rules = append(rules, setuprepo.InitialWorldRule{ID: rule.ID, SystemID: rule.SystemID, Title: rule.Title, Category: rule.Category, Severity: rule.Severity, Statement: rule.Statement, Preconditions: rule.Preconditions, Costs: rule.Costs, ForbiddenResults: rule.ForbiddenResults, Exceptions: rule.Exceptions, Tags: rule.Tags, Visibility: rule.Visibility, Status: rule.Status, ExceptionOf: rule.ExceptionOf})
	}
	return systems, rules, nil
}
func ensureFourOpeningChoices(in []string) []string {
	fallbacks := []string{
		"Осмотреться внимательнее и собрать больше информации",
		"Поговорить с кем-нибудь поблизости и задать вопросы",
		"Проверить ближайшее окружение на необычные детали",
		"Отойти в сторону и обдумать ситуацию прежде чем действовать",
	}
	out := make([]string, 0, 4)
	seen := map[string]struct{}{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || len(out) >= 4 {
			return
		}
		k := strings.ToLower(v)
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		out = append(out, v)
	}
	for _, v := range in {
		add(v)
	}
	for _, v := range fallbacks {
		add(v)
	}
	return out
}

func keys(m map[setup.ComponentKey]bool) []string {
	out := make([]string, 0, len(m))
	for _, k := range setup.AllKeys {
		if m[k] {
			out = append(out, string(k))
		}
	}
	return out
}
