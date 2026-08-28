package generation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	"github.com/local/interactive-ai-story/backend/internal/application/canon"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type memSink struct{ phases []Phase }

func (s *memSink) Publish(_ context.Context, u Update) { s.phases = append(s.phases, u.Phase) }

type targetSource struct{}

func (targetSource) CurrentWriteTarget(context.Context, timeline.ID) (generationtarget.Target, error) {
	return generationtarget.Target{SceneID: id.MustParse("00000000-0000-4000-8000-000000000090"), NextBeatPosition: 2}, nil
}

type parallelBarrierLLM struct {
	responses map[string][]byte
	started   chan string
	release   chan struct{}
	mu        sync.Mutex
	calls     []string
}

type dependencyGraphLLM struct {
	responses      map[string][]byte
	worldStarted   chan struct{}
	worldRelease   chan struct{}
	choicesStarted chan struct{}
}

func (d *dependencyGraphLLM) Identity() aiport.ProviderIdentity {
	return aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "google_gemini", Model: "dependency-test", Profile: "hosted"}
}

func (d *dependencyGraphLLM) Generate(ctx context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	switch request.Role {
	case "world_evaluator":
		close(d.worldStarted)
		select {
		case <-d.worldRelease:
		case <-ctx.Done():
			return aiport.StoryResponse{}, ctx.Err()
		}
	case "choices":
		close(d.choicesStarted)
	}
	response, ok := d.responses[request.Role]
	if !ok {
		return aiport.StoryResponse{}, aiport.ErrInvalidOutput
	}
	return aiport.StoryResponse{Output: append([]byte(nil), response...)}, nil
}

func (p *parallelBarrierLLM) Identity() aiport.ProviderIdentity {
	return aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "google_gemini", Model: "parallel-test", Profile: "hosted"}
}

func (p *parallelBarrierLLM) Generate(ctx context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	p.mu.Lock()
	p.calls = append(p.calls, request.Role)
	p.mu.Unlock()
	if request.Role == "world_evaluator" || request.Role == "state_evaluator" || request.Role == "quest_evaluator" {
		p.started <- request.Role
		select {
		case <-p.release:
		case <-ctx.Done():
			return aiport.StoryResponse{}, ctx.Err()
		}
	}
	response, ok := p.responses[request.Role]
	if !ok {
		return aiport.StoryResponse{}, aiport.ErrInvalidOutput
	}
	return aiport.StoryResponse{Output: append([]byte(nil), response...)}, nil
}

type appender struct {
	head   int64
	events []event.StoredEvent
}

func (a *appender) AppendSemantic(_ context.Context, tid timeline.ID, expected int64, p []event.PendingEvent) ([]event.StoredEvent, error) {
	if expected != a.head {
		return nil, canon.ErrHeadMoved
	}
	out := make([]event.StoredEvent, len(p))
	for i, x := range p {
		a.head++
		out[i] = event.StoredEvent{ID: event.ID(mustID(100 + i)), TimelineID: tid, Seq: a.head, Type: x.Type, SchemaVersion: x.SchemaVersion, Payload: x.Payload, GenerationID: x.GenerationID, CreatedAt: time.Unix(1, 0)}
	}
	a.events = append(a.events, out...)
	return out, nil
}
func mustID(n int) id.ID {
	const digits = "0123456789"
	return id.MustParse("00000000-0000-4000-8000-00000000000" + string(digits[n%10]))
}

func scripted() *fakeai.ScriptedStoryLLM {
	return fakeai.NewScriptedStoryLLM(map[string][]byte{
		"action_interpreter": []byte(`{"intent":"try_to_open_door"}`),
		"director":           []byte(`{"goal":"door resists; reveal a clue"}`),
		"pacing":             []byte(`{"transition":"continue_scene"}`),
		"world_evaluator":    []byte(`{"changes":[]}`),
		"writer":             []byte(`{"text":"Первый абзац развивает действие.\n\nВторой абзац уточняет окружение.\n\nТретий абзац показывает реакцию.\n\nЧетвёртый абзац содержит диалог.\n\nПятый абзац добавляет последствие.\n\nШестой абзац усиливает напряжение.\n\nСедьмой абзац заканчивает моментом выбора."}`),
		"state_evaluator":    []byte(`{"changes":[]}`),
		"quest_evaluator":    []byte(`{"changes":[]}`),
		"choices":            []byte(`{"choices":[{"id":"inspect","label":"Inspect the scratch"},{"id":"listen","label":"Listen at the door"},{"id":"tools","label":"Check your tools"},{"id":"leave","label":"Step away"}]}`),
	})
}

func TestObjectiveCompletionBecomesCanonBeforeChoices(t *testing.T) {
	llm := scripted()
	objectiveID := "00000000-0000-4000-8000-000000000099"
	llm.Responses["quest_evaluator"] = []byte(`{"changes":[{"operation":"complete","objectiveId":"` + objectiveID + `","progress":100,"evidence":"The opened door revealed the marked room."}]}`)
	c := &appender{}
	p := Pipeline{LLM: llm, Canon: c, Targets: staticTarget{target: generationtarget.Target{SceneID: id.MustParse("00000000-0000-4000-8000-000000000090"), NextBeatPosition: 2, Objectives: []generationtarget.Objective{{ID: objectiveID, Scope: "minor", Title: "Open the marked door", SuccessCriteria: "The door is opened", Status: "active", Progress: 40}}}}}
	events, err := p.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000010"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000011")), Text: "I open the door"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[2].Type != "objective_updated" || events[3].Type != "choices_ready" {
		t.Fatalf("unexpected objective event order: %+v", events)
	}
	var payload struct {
		Status   string `json:"status"`
		Progress int    `json:"progress"`
		Evidence string `json:"evidence"`
	}
	if err := json.Unmarshal(events[2].Payload, &payload); err != nil || payload.Status != "completed" || payload.Progress != 100 || payload.Evidence == "" {
		t.Fatalf("invalid completion payload: %s", events[2].Payload)
	}
}

func TestPacingStartsNewSceneAndExpiresScopedInstructions(t *testing.T) {
	llm := scripted()
	llm.Responses["pacing"] = []byte(`{"transition":"new_scene","sceneGoal":"Договориться со смотрителем","sceneMood":"настороженное ожидание"}`)
	chapterID := id.MustParse("00000000-0000-4000-8000-000000000081")
	sceneID := id.MustParse("00000000-0000-4000-8000-000000000082")
	instructionID := id.MustParse("00000000-0000-4000-8000-000000000083")
	c := &appender{}
	p := Pipeline{
		LLM: llm, Canon: c,
		Targets:      staticTarget{target: generationtarget.Target{ChapterID: chapterID, SceneID: sceneID, ChapterNumber: 1, SceneNumber: 2, ChapterSceneCount: 2, ChapterBeatCount: 8, NextBeatPosition: 5}},
		Instructions: instructionSource{values: []narrative.DirectorInstruction{{ID: instructionID, Scope: narrative.ScopeScene, Status: "active"}}},
	}
	events, err := p.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000084"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000085")), Text: "Перейти в кабинет смотрителя"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"player_action_attempted", "scene_completed", "scene_started", "beat_committed", "director_instructions_expired", "choices_ready"}
	if len(events) != len(want) {
		t.Fatalf("unexpected event count: %+v", events)
	}
	for index, eventType := range want {
		if events[index].Type != eventType {
			t.Fatalf("event %d = %s, want %s", index, events[index].Type, eventType)
		}
	}
	var started struct {
		SceneID   id.ID  `json:"sceneId"`
		ChapterID id.ID  `json:"chapterId"`
		Number    int    `json:"number"`
		Goal      string `json:"goal"`
	}
	if err = json.Unmarshal(events[2].Payload, &started); err != nil || started.SceneID == sceneID || started.ChapterID != chapterID || started.Number != 3 || started.Goal == "" {
		t.Fatalf("invalid new scene: %s", events[2].Payload)
	}
	var beat struct {
		SceneID  id.ID  `json:"sceneId"`
		Position int    `json:"position"`
		Kind     string `json:"kind"`
	}
	if err = json.Unmarshal(events[3].Payload, &beat); err != nil || beat.SceneID != started.SceneID || beat.Position != 1 || beat.Kind != "transition" {
		t.Fatalf("beat was not attached to new scene: %s", events[3].Payload)
	}
}

func TestPacingHardCapStartsNewChapter(t *testing.T) {
	llm := scripted()
	// Even a weak model asking to continue is overridden after sixty chapter beats.
	llm.Responses["pacing"] = []byte(`{"transition":"continue_scene"}`)
	chapterID := id.MustParse("00000000-0000-4000-8000-000000000086")
	sceneID := id.MustParse("00000000-0000-4000-8000-000000000087")
	c := &appender{}
	p := Pipeline{LLM: llm, Canon: c, Targets: staticTarget{target: generationtarget.Target{ChapterID: chapterID, SceneID: sceneID, ChapterNumber: 1, SceneNumber: 1, ChapterSceneCount: 1, ChapterBeatCount: 60, NextBeatPosition: 61}}}
	events, err := p.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000088"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000089")), Text: "Продолжить разговор"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"player_action_attempted", "scene_completed", "chapter_completed", "chapter_created", "scene_started", "beat_committed", "choices_ready"}
	if len(events) != len(want) {
		t.Fatalf("unexpected hard-cap events: %+v", events)
	}
	for index, eventType := range want {
		if events[index].Type != eventType {
			t.Fatalf("event %d = %s, want %s", index, events[index].Type, eventType)
		}
	}
}

func TestWorldEvaluatorCreatesDurableCharacterAndLocationBeforeChoices(t *testing.T) {
	llm := scripted()
	llm.Responses["world_evaluator"] = []byte(`{"changes":[
		{"type":"upsert_character","name":"Смотритель Илья","age":43,"role":"хранитель архива","personality":"настороженный и наблюдательный","relationship":"пока не доверяет герою","visualAnchorEn":"lean middle-aged archivist with silver temples and a dark wool coat","mood":"встревожен","currentGoal":"не допустить героя в закрытое хранилище"},
		{"type":"upsert_location","name":"Закрытое хранилище","description":"Подземный архив за тяжёлой дверью с латунным номером 17.","visualAnchorEn":"underground archive vault behind a heavy brass-numbered door"}
	]}`)
	c := &appender{}
	p := Pipeline{LLM: llm, Canon: c, Targets: targetSource{}}
	events, err := p.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000018"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000019")), Text: "Я представляюсь смотрителю"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"player_action_attempted", "beat_committed", "character_world_upserted", "location_world_upserted", "choices_ready"}
	if len(events) != len(want) {
		t.Fatalf("unexpected world event count: %+v", events)
	}
	for index, eventType := range want {
		if events[index].Type != eventType {
			t.Fatalf("event %d = %s, want %s", index, events[index].Type, eventType)
		}
	}
	var character struct {
		CharacterID string `json:"characterId"`
		Name        string `json:"name"`
		Kind        string `json:"kind"`
	}
	var location struct {
		LocationID string `json:"locationId"`
		Name       string `json:"name"`
	}
	if json.Unmarshal(events[2].Payload, &character) != nil || character.CharacterID == "" || character.Name != "Смотритель Илья" || character.Kind != "persistent_npc" {
		t.Fatalf("invalid durable character event: %s", events[2].Payload)
	}
	if json.Unmarshal(events[3].Payload, &location) != nil || location.LocationID == "" || location.Name != "Закрытое хранилище" {
		t.Fatalf("invalid durable location event: %s", events[3].Payload)
	}
}

func TestRejectedAdvisoryEvaluatorsDoNotDiscardValidStoryBeat(t *testing.T) {
	llm := scripted()
	llm.Responses["world_evaluator"] = []byte(`{"changes":[{"type":"upsert_character","name":"Ошибка","age":999}]}`)
	llm.Responses["state_evaluator"] = []byte(`{"changes":[{"operation":"create","category":"currency","name":"Монеты","quantity":-10,"evidence":"ошибочное значение"}]}`)
	llm.Responses["quest_evaluator"] = []byte(`{"changes":[{"operation":"create","scope":"minor","kind":"task","title":"Сиротская задача","successCriteria":"Невозможно"}]}`)
	c := &appender{}
	p := Pipeline{LLM: llm, Canon: c, Targets: targetSource{}}
	events, err := p.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000020"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000021")), Text: "Я продолжаю путь"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"player_action_attempted", "beat_committed", "choices_ready"}
	if len(events) != len(want) {
		t.Fatalf("invalid advisory changes must be dropped, got %+v", events)
	}
	for index, eventType := range want {
		if events[index].Type != eventType {
			t.Fatalf("event %d = %s, want %s", index, events[index].Type, eventType)
		}
	}
}

func TestHostedProviderRunsIndependentPostWriteEvaluatorsInParallel(t *testing.T) {
	base := scripted()
	llm := &parallelBarrierLLM{responses: base.Responses, started: make(chan string, 3), release: make(chan struct{})}
	c := &appender{}
	pipeline := Pipeline{LLM: llm, Canon: c, Targets: targetSource{}}
	result := make(chan error, 1)
	go func() {
		_, err := pipeline.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000022"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000023")), Text: "Я осматриваю помещение"})
		result <- err
	}()

	started := map[string]bool{}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for len(started) < 3 {
		select {
		case role := <-llm.started:
			started[role] = true
		case <-deadline.C:
			close(llm.release)
			t.Fatalf("independent evaluators did not reach the same parallel barrier: %#v", started)
		}
	}
	close(llm.release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("parallel generation did not complete after evaluators were released")
	}
}

func TestHostedChoicesWaitOnlyForTheirDependencies(t *testing.T) {
	base := scripted()
	llm := &dependencyGraphLLM{
		responses:      base.Responses,
		worldStarted:   make(chan struct{}),
		worldRelease:   make(chan struct{}),
		choicesStarted: make(chan struct{}),
	}
	pipeline := Pipeline{LLM: llm, Canon: &appender{}, Targets: targetSource{}}
	result := make(chan error, 1)
	go func() {
		_, err := pipeline.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000024"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000025")), Text: "Я выбираю дальнейший путь"})
		result <- err
	}()

	select {
	case <-llm.worldStarted:
	case <-time.After(time.Second):
		close(llm.worldRelease)
		t.Fatal("world evaluator did not start")
	}
	select {
	case <-llm.choicesStarted:
		// Choices may start as soon as journal and objective changes are ready,
		// even while the independent world branch is still running.
	case <-time.After(time.Second):
		close(llm.worldRelease)
		t.Fatal("choices unnecessarily waited for the independent world evaluator")
	}
	close(llm.worldRelease)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("dependency graph generation did not complete")
	}
}

func TestHeroJournalCreatesAbilityAndInventoryItemBeforeChoices(t *testing.T) {
	llm := scripted()
	llm.Responses["state_evaluator"] = []byte(`{"changes":[
		{"operation":"create","category":"ability","name":"Эхо-память","description":"Восстанавливает недавний звук по следу нейроимпланта","level":"нестабильно","status":"active","evidence":"Герой воспроизвёл исчезнувший голос из коридора.","tags":["нейроимплант","анализ"]},
		{"operation":"create","category":"item","name":"Ключ архива","description":"Латунный ключ с номером 17","quantity":1,"status":"active","evidence":"Герой поднял ключ и положил его в карман.","tags":["ключ"]}
	]}`)
	c := &appender{}
	p := Pipeline{LLM: llm, Canon: c, Targets: targetSource{}}
	events, err := p.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000016"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000017")), Text: "Я подбираю ключ"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 || events[2].Type != "journal_entry_created" || events[3].Type != "journal_entry_created" || events[4].Type != "choices_ready" {
		t.Fatalf("journal changes must be committed before choices: %+v", events)
	}
	var ability, item struct {
		EntryID  string   `json:"entryId"`
		Category string   `json:"category"`
		Quantity int      `json:"quantity"`
		Status   string   `json:"status"`
		Evidence string   `json:"evidence"`
		Tags     []string `json:"tags"`
	}
	if json.Unmarshal(events[2].Payload, &ability) != nil || json.Unmarshal(events[3].Payload, &item) != nil {
		t.Fatal("could not decode journal events")
	}
	if ability.EntryID == "" || ability.Category != "ability" || ability.Status != "active" || ability.Evidence == "" || len(ability.Tags) != 2 {
		t.Fatalf("invalid ability event: %s", events[2].Payload)
	}
	if item.EntryID == "" || item.Category != "item" || item.Quantity != 1 || item.Status != "active" || item.Evidence == "" {
		t.Fatalf("invalid inventory event: %s", events[3].Payload)
	}
}

func TestHeroJournalUpdatesQuantityAndPreservesRemovedEntry(t *testing.T) {
	itemID := "00000000-0000-4000-8000-000000000118"
	current := []generationtarget.JournalEntry{{ID: itemID, Category: "item", Name: "Медпакет", Description: "Полевой набор", Quantity: 2, Status: "active", Tags: []string{"лечение"}}}
	remaining := 1
	events, changes, err := validateJournalChanges(current, []journalChange{{Operation: "update", EntryID: itemID, Quantity: &remaining, Evidence: "Один медпакет использован для перевязки."}})
	if err != nil || len(events) != 1 || len(changes) != 1 {
		t.Fatalf("quantity update failed: events=%d changes=%d err=%v", len(events), len(changes), err)
	}
	var updated struct {
		Quantity int    `json:"quantity"`
		Status   string `json:"status"`
	}
	if json.Unmarshal(events[0].Payload, &updated) != nil || updated.Quantity != 1 || updated.Status != "active" {
		t.Fatalf("invalid quantity update: %s", events[0].Payload)
	}
	events, _, err = validateJournalChanges(current, []journalChange{{Operation: "remove", EntryID: itemID, Evidence: "Последний медпакет передан проводнику."}})
	if err != nil || len(events) != 1 {
		t.Fatalf("item removal failed: %v", err)
	}
	if json.Unmarshal(events[0].Payload, &updated) != nil || updated.Quantity != 0 || updated.Status != "inactive" {
		t.Fatalf("removed item must remain in history: %s", events[0].Payload)
	}
}

func TestHeroJournalRejectsUnevidencedChange(t *testing.T) {
	_, _, err := validateJournalChanges(nil, []journalChange{{Operation: "create", Category: "ability", Name: "Телепатия"}})
	if !errors.Is(err, ErrRejectedProposal) {
		t.Fatalf("unevidenced ability must not enter Canon: %v", err)
	}
}

func TestHeroJournalTracksCurrencyAndRejectsNegativeBalance(t *testing.T) {
	currencyID := "00000000-0000-4000-8000-000000000119"
	current := []generationtarget.JournalEntry{{ID: currencyID, Category: "currency", Name: "кредитов", Quantity: 120, Status: "active", Evidence: "Баланс показан терминалом."}}
	remaining := 75
	events, _, err := validateJournalChanges(current, []journalChange{{Operation: "update", EntryID: currencyID, Quantity: &remaining, Evidence: "Герой заплатил 45 кредитов за пропуск."}})
	if err != nil || len(events) != 1 {
		t.Fatalf("currency deduction failed: events=%d err=%v", len(events), err)
	}
	var payload struct {
		Category string `json:"category"`
		Quantity int    `json:"quantity"`
		Status   string `json:"status"`
	}
	if json.Unmarshal(events[0].Payload, &payload) != nil || payload.Category != "currency" || payload.Quantity != 75 || payload.Status != "active" {
		t.Fatalf("invalid currency update: %s", events[0].Payload)
	}
	negative := -1
	if _, _, err = validateJournalChanges(current, []journalChange{{Operation: "update", EntryID: currencyID, Quantity: &negative, Evidence: "Несуществующий перерасход"}}); !errors.Is(err, ErrRejectedProposal) {
		t.Fatalf("negative balance must be rejected: %v", err)
	}
}

func TestQuestEvaluatorCreatesMajorQuestAndFirstStageInOneTurn(t *testing.T) {
	llm := scripted()
	llm.Responses["quest_evaluator"] = []byte(`{"changes":[
		{"operation":"create","reference":"signal_quest","scope":"global","kind":"quest","title":"Раскрыть источник сигнала","successCriteria":"Источник сигнала найден и его назначение установлено","status":"active","progress":0},
		{"operation":"create","parentObjectiveId":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","parentReference":"signal_quest","scope":"minor","kind":"task","title":"Добраться до радиобашни","successCriteria":"Герой входит в диспетчерскую радиобашни","status":"active","progress":0}
	]}`)
	c := &appender{}
	p := Pipeline{LLM: llm, Canon: c, Targets: targetSource{}}
	events, err := p.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000012"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000013")), Text: "Я иду к башне"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 || events[2].Type != "objective_created" || events[3].Type != "objective_created" {
		t.Fatalf("major quest and stage were not committed atomically: %+v", events)
	}
	var major, stage struct {
		ObjectiveID       string `json:"objectiveId"`
		ParentObjectiveID string `json:"parentObjectiveId"`
		Scope             string `json:"scope"`
		Kind              string `json:"kind"`
	}
	if json.Unmarshal(events[2].Payload, &major) != nil || json.Unmarshal(events[3].Payload, &stage) != nil {
		t.Fatal("could not decode quest events")
	}
	if major.Scope != "global" || major.Kind != "quest" || stage.Scope != "minor" || stage.Kind != "task" || stage.ParentObjectiveID != major.ObjectiveID {
		t.Fatalf("broken quest hierarchy: major=%+v stage=%+v", major, stage)
	}
}

func TestQuestEvaluatorCreatesSideQuestWithInheritedStageType(t *testing.T) {
	events, normalized, err := validateObjectiveChanges(nil, []objectiveChange{
		{Operation: "create", Reference: "smith_job", Scope: "global", Kind: "quest", QuestType: "side", Title: "Помочь кузнецу", SuccessCriteria: "Заказ кузнеца завершён", Status: "active"},
		{Operation: "create", ParentReference: "smith_job", Scope: "minor", Kind: "task", Title: "Проверить испорченную руду", SuccessCriteria: "Причина дефекта установлена", Status: "active"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || len(normalized) != 2 || normalized[0].QuestType != "side" || normalized[1].QuestType != "side" {
		t.Fatalf("side quest type was not inherited by its stage: %#v", normalized)
	}
	var stage struct {
		QuestType         string `json:"questType"`
		ParentObjectiveID string `json:"parentObjectiveId"`
	}
	if err = json.Unmarshal(events[1].Payload, &stage); err != nil || stage.QuestType != "side" || stage.ParentObjectiveID != normalized[0].ObjectiveID {
		t.Fatalf("invalid side stage payload: %s", events[1].Payload)
	}
}

func TestNewUnclassifiedQuestDefaultsToSideAndCannotMixStageType(t *testing.T) {
	events, normalized, err := validateObjectiveChanges(nil, []objectiveChange{{Operation: "create", Scope: "global", Kind: "quest", Title: "Необязательная находка", SuccessCriteria: "Тайна находки раскрыта", Status: "active"}})
	if err != nil || len(events) != 1 || normalized[0].QuestType != "side" {
		t.Fatalf("unclassified dynamic quest must default to side: events=%d changes=%#v err=%v", len(events), normalized, err)
	}
	majorID := "00000000-0000-4000-8000-000000000124"
	current := []generationtarget.Objective{{ID: majorID, Scope: "global", Kind: "quest", QuestType: "side", Title: "Побочная линия", Status: "active"}}
	_, _, err = validateObjectiveChanges(current, []objectiveChange{{Operation: "create", ParentObjectiveID: majorID, Scope: "minor", Kind: "task", QuestType: "main", Title: "Чужой этап", SuccessCriteria: "Этап выполнен", Status: "active"}})
	if !errors.Is(err, ErrRejectedProposal) {
		t.Fatalf("stage cannot claim a different quest type, got %v", err)
	}
}

func TestCompletingStageDerivesMajorQuestProgress(t *testing.T) {
	majorID := "00000000-0000-4000-8000-000000000120"
	stageID := "00000000-0000-4000-8000-000000000121"
	current := []generationtarget.Objective{
		{ID: majorID, Scope: "global", Kind: "quest", Title: "Раскрыть заговор", SuccessCriteria: "Заговорщики разоблачены", Status: "active", Progress: 10},
		{ID: stageID, ParentObjectiveID: majorID, Scope: "minor", Kind: "milestone", Title: "Получить список имён", SuccessCriteria: "Список прочитан", Status: "active", Progress: 30},
	}
	events, normalized, err := validateObjectiveChanges(current, []objectiveChange{{Operation: "complete", ObjectiveID: stageID, Evidence: "Список найден и прочитан."}})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || len(normalized) != 2 {
		t.Fatalf("stage completion must also update its major quest: events=%d changes=%d", len(events), len(normalized))
	}
	var parentUpdate struct {
		ObjectiveID string `json:"objectiveId"`
		Status      string `json:"status"`
		Progress    int    `json:"progress"`
	}
	if err = json.Unmarshal(events[1].Payload, &parentUpdate); err != nil || parentUpdate.ObjectiveID != majorID || parentUpdate.Status != "active" || parentUpdate.Progress != 99 {
		t.Fatalf("major quest progress was not derived from its stage: %s", events[1].Payload)
	}
}

func TestQuestCanCompleteCurrentStageAndAddNextStageDynamically(t *testing.T) {
	majorID := "00000000-0000-4000-8000-000000000125"
	stageID := "00000000-0000-4000-8000-000000000126"
	current := []generationtarget.Objective{
		{ID: majorID, Scope: "global", Kind: "quest", Title: "Найти пропавшую экспедицию", SuccessCriteria: "Экспедиция найдена", Status: "active"},
		{ID: stageID, ParentObjectiveID: majorID, Scope: "minor", Kind: "task", Title: "Осмотреть брошенный лагерь", SuccessCriteria: "Лагерь осмотрен", Status: "active"},
	}
	events, _, err := validateObjectiveChanges(current, []objectiveChange{
		{Operation: "complete", ObjectiveID: stageID, Evidence: "В лагере найдены следы ухода на север."},
		{Operation: "create", ParentObjectiveID: majorID, ParentReference: "human-readable redundant hint", Scope: "minor", Kind: "task", Title: "Проследовать по северному следу", SuccessCriteria: "Источник следов найден", Status: "active"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Type != "objective_updated" || events[1].Type != "objective_created" || events[2].Type != "objective_updated" {
		t.Fatalf("quest did not advance from one stage to the next: %+v", events)
	}
	var next struct {
		ParentObjectiveID string `json:"parentObjectiveId"`
		Status            string `json:"status"`
	}
	if err = json.Unmarshal(events[1].Payload, &next); err != nil || next.ParentObjectiveID != majorID || next.Status != "active" {
		t.Fatalf("new stage is not linked to the existing quest: %s", events[1].Payload)
	}
}

func TestQuestValidationRejectsOrphanStageAndPrematureMajorClosure(t *testing.T) {
	majorID := "00000000-0000-4000-8000-000000000130"
	stageID := "00000000-0000-4000-8000-000000000131"
	current := []generationtarget.Objective{
		{ID: majorID, Scope: "global", Kind: "quest", Title: "Спасти экспедицию", SuccessCriteria: "Экспедиция вернулась", Status: "active"},
		{ID: stageID, ParentObjectiveID: majorID, Scope: "minor", Kind: "task", Title: "Найти лагерь", SuccessCriteria: "Лагерь найден", Status: "active"},
	}
	if _, _, err := validateObjectiveChanges(current, []objectiveChange{{Operation: "create", Scope: "minor", Kind: "task", Title: "Сиротская задача", SuccessCriteria: "Что-то сделано"}}); !errors.Is(err, ErrRejectedProposal) {
		t.Fatalf("orphan stage must be rejected, got %v", err)
	}
	if _, _, err := validateObjectiveChanges(current, []objectiveChange{{Operation: "complete", ObjectiveID: majorID, Evidence: "Финал"}}); !errors.Is(err, ErrRejectedProposal) {
		t.Fatalf("major quest with an active stage must not close, got %v", err)
	}
}

func TestQuestValidationDropsHistoricalAndRunawayCreations(t *testing.T) {
	majorID := "00000000-0000-4000-8000-000000000132"
	current := []generationtarget.Objective{
		{ID: majorID, Scope: "global", Kind: "quest", QuestType: "main", Title: "Вернуться домой", Status: "active"},
		{ID: "00000000-0000-4000-8000-000000000133", ParentObjectiveID: majorID, Scope: "minor", Kind: "task", QuestType: "main", Title: "Найти карту", Status: "active"},
		{ID: "00000000-0000-4000-8000-000000000134", ParentObjectiveID: majorID, Scope: "minor", Kind: "task", QuestType: "main", Title: "Открыть портал", Status: "active"},
		{ID: "00000000-0000-4000-8000-000000000135", ParentObjectiveID: majorID, Scope: "minor", Kind: "event", QuestType: "main", Title: "Дождаться затмения", Status: "active"},
	}
	events, normalized, err := validateObjectiveChanges(current, []objectiveChange{
		{Operation: "create", ParentObjectiveID: majorID, Scope: "minor", Kind: "task", Title: "Лишняя четвёртая задача", SuccessCriteria: "Задача выполнена", Status: "active"},
		{Operation: "create", ParentObjectiveID: majorID, Scope: "minor", Kind: "task", Title: "Уже сделанное действие", SuccessCriteria: "Действие выполнено", Status: "completed", Progress: 100, Evidence: "Уже сделано"},
		{Operation: "create", ParentObjectiveID: majorID, Scope: "minor", Kind: "task", Title: "Найти карту", SuccessCriteria: "Карта найдена", Status: "active"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 || len(normalized) != 0 {
		t.Fatalf("runaway or historical quest creations entered Canon: events=%d changes=%#v", len(events), normalized)
	}
}

func TestQuestHierarchyInfluencesDirectorWriterAndChoices(t *testing.T) {
	majorID := "00000000-0000-4000-8000-000000000140"
	stageID := "00000000-0000-4000-8000-000000000141"
	llm := scripted()
	c := &appender{}
	target := generationtarget.Target{SceneID: id.MustParse("00000000-0000-4000-8000-000000000090"), NextBeatPosition: 2, Objectives: []generationtarget.Objective{
		{ID: majorID, Scope: "global", Kind: "quest", Title: "Остановить шторм", SuccessCriteria: "Шторм остановлен", Status: "active"},
		{ID: stageID, ParentObjectiveID: majorID, Scope: "minor", Kind: "event", Title: "Дождаться затмения", SuccessCriteria: "Затмение началось", Status: "active"},
	}}
	p := Pipeline{LLM: llm, Canon: c, Targets: staticTarget{target: target}}
	if _, err := p.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000014"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000015")), Text: "Я наблюдаю за небом"}); err != nil {
		t.Fatal(err)
	}
	checked := map[string]bool{"director": false, "writer": false, "quest_evaluator": false, "choices": false}
	for _, request := range llm.Requests {
		if _, ok := checked[request.Role]; !ok {
			continue
		}
		input := string(request.Input)
		if !strings.Contains(input, `"quests"`) || !strings.Contains(input, `"stages"`) || !strings.Contains(input, stageID) {
			t.Fatalf("role %s did not receive the quest hierarchy: %s", request.Role, input)
		}
		checked[request.Role] = true
	}
	for role, found := range checked {
		if !found {
			t.Fatalf("quest hierarchy was not verified for role %s", role)
		}
	}
}

func TestHeroJournalInfluencesDirectorWriterAndChoices(t *testing.T) {
	entryID := "00000000-0000-4000-8000-000000000148"
	llm := scripted()
	c := &appender{}
	target := generationtarget.Target{SceneID: id.MustParse("00000000-0000-4000-8000-000000000090"), NextBeatPosition: 2, Journal: []generationtarget.JournalEntry{{ID: entryID, Category: "item", Name: "Архивный ключ", Quantity: 1, Status: "active", Evidence: "Ключ был найден."}}}
	p := Pipeline{LLM: llm, Canon: c, Targets: staticTarget{target: target}}
	if _, err := p.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000149"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000150")), Text: "Я осматриваю дверь"}); err != nil {
		t.Fatal(err)
	}
	checked := map[string]bool{"director": false, "writer": false, "state_evaluator": false, "choices": false}
	for _, request := range llm.Requests {
		if _, ok := checked[request.Role]; !ok {
			continue
		}
		if !strings.Contains(string(request.Input), entryID) {
			t.Fatalf("role %s did not receive hero journal: %s", request.Role, request.Input)
		}
		checked[request.Role] = true
	}
	for role, found := range checked {
		if !found {
			t.Fatalf("hero journal was not verified for role %s", role)
		}
	}
}

type staticTarget struct{ target generationtarget.Target }

func (s staticTarget) CurrentWriteTarget(context.Context, timeline.ID) (generationtarget.Target, error) {
	return s.target, nil
}

func TestVerticalSliceCommitsOnlyAfterAllRolesValidate(t *testing.T) {
	c := &appender{}
	sink := &memSink{}
	p := Pipeline{LLM: scripted(), Canon: c, Sink: sink, Targets: targetSource{}}
	g := id.MustParse("00000000-0000-4000-8000-000000000010")
	tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000011"))
	ev, err := p.Run(context.Background(), g, PlayerAction{TimelineID: tid, ExpectedHead: 0, Text: "I open the door"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 3 || c.head != 3 {
		t.Fatalf("expected atomic 3-event commit, got %d head %d", len(ev), c.head)
	}
	if ev[0].Type != "player_action_attempted" || ev[1].Type != "beat_committed" || ev[2].Type != "choices_ready" {
		t.Fatal("unexpected semantic event order")
	}
	var beat struct {
		BeatID id.ID `json:"beatId"`
	}
	if err := json.Unmarshal(ev[1].Payload, &beat); err != nil || beat.BeatID.IsZero() {
		t.Fatalf("beat_committed must contain beat id: %s", ev[1].Payload)
	}
	var ready struct {
		BeatID  id.ID    `json:"beatId"`
		Choices []string `json:"choices"`
	}
	if err := json.Unmarshal(ev[2].Payload, &ready); err != nil || len(ready.Choices) != 4 {
		t.Fatalf("choices_ready must contain four reader labels: %s", ev[2].Payload)
	}
	if ready.BeatID != beat.BeatID {
		t.Fatalf("choices_ready must belong to committed beat: beat=%s choices=%s", beat.BeatID, ready.BeatID)
	}
	for _, e := range ev {
		if e.GenerationID == nil || id.ID(*e.GenerationID) != g {
			t.Fatal("generation provenance missing")
		}
	}
	if sink.phases[len(sink.phases)-1] != PhaseCompleted {
		t.Fatal("generation did not complete")
	}
}

func TestMalformedLLMJSONNeverMutatesCanon(t *testing.T) {
	llm := scripted()
	llm.Responses["director"] = []byte(`{broken`)
	c := &appender{}
	p := Pipeline{LLM: llm, Canon: c, Targets: targetSource{}}
	_, err := p.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000020"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000021")), Text: "x"})
	if !errors.Is(err, ErrMalformedProposal) {
		t.Fatalf("expected malformed proposal: %v", err)
	}
	if c.head != 0 || len(c.events) != 0 {
		t.Fatal("invalid AI output mutated Canon")
	}
}
func TestStaleHeadRejectsWholeProposal(t *testing.T) {
	c := &appender{head: 4}
	p := Pipeline{LLM: scripted(), Canon: c, Targets: targetSource{}}
	_, err := p.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000030"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000031")), ExpectedHead: 3, Text: "x"})
	if !errors.Is(err, canon.ErrHeadMoved) {
		t.Fatalf("expected stale head: %v", err)
	}
	if len(c.events) != 0 || c.head != 4 {
		t.Fatal("stale generation partially committed")
	}
}

func TestControlledRepairCanRecoverMalformedRoleOutput(t *testing.T) {
	llm := scripted()
	llm.Responses["director"] = []byte(`broken`)
	llm.Responses["structured_repair"] = []byte(`{"goal":"repaired goal"}`)
	c := &appender{}
	p := Pipeline{LLM: llm, MaxRepairs: 1, Canon: c, Targets: targetSource{}}
	_, err := p.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000040"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000041")), Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if c.head != 3 {
		t.Fatal("repaired generation did not commit atomically")
	}
}
func TestRepairBudgetExhaustionLeavesCanonUntouched(t *testing.T) {
	llm := scripted()
	llm.Responses["director"] = []byte(`broken`)
	llm.Responses["structured_repair"] = []byte(`still broken`)
	c := &appender{}
	p := Pipeline{LLM: llm, MaxRepairs: 1, Canon: c, Targets: targetSource{}}
	_, err := p.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000050"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000051")), Text: "x"})
	if !errors.Is(err, ErrMalformedProposal) {
		t.Fatalf("expected malformed after repair: %v", err)
	}
	if c.head != 0 {
		t.Fatal("failed repair mutated Canon")
	}
}

type instructionSource struct {
	values []narrative.DirectorInstruction
}

func (i instructionSource) ActiveInstructions(context.Context, timeline.ID) ([]narrative.DirectorInstruction, error) {
	return i.values, nil
}
func TestHardInstructionReachesDirectorRoleContext(t *testing.T) {
	llm := scripted()
	c := &appender{}
	p := Pipeline{LLM: llm, Canon: c, Targets: targetSource{}, Instructions: instructionSource{values: []narrative.DirectorInstruction{{Text: "keep secret", Scope: narrative.ScopeNextBeat, Priority: narrative.PriorityHard}}}}
	_, e := p.Run(context.Background(), id.MustParse("00000000-0000-4000-8000-000000000060"), PlayerAction{TimelineID: timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000061")), Text: "x"})
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, role := range llm.Calls {
		if role == "director" {
			found = true
		}
	}
	if !found {
		t.Fatal("director role was not called")
	}
}
