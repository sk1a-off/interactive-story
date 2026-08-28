package storysetup

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/local/interactive-ai-story/backend/internal/adapters/fakeai"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/setup"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/setuprepo"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

type memRepo struct {
	story      story.Story
	stories    []setuprepo.StoryOverview
	components map[setup.ComponentKey]setup.Component
	started    bool
	start      setuprepo.StartMaterialization
	deleted    story.ID
}

type onlineSetupBarrierLLM struct {
	started chan string
	release chan struct{}
	mu      sync.Mutex
	calls   []string
}

type setupDAGLLM struct {
	visualStarted  chan struct{}
	visualRelease  chan struct{}
	openingStarted chan struct{}
	openingOnce    sync.Once
}

type onlineAssistBarrierLLM struct {
	started chan string
	release chan struct{}
}

func (l *onlineAssistBarrierLLM) Identity() aiport.ProviderIdentity {
	return aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "google_gemini", Model: "parallel-assist-test", Profile: "hosted"}
}

func (l *onlineAssistBarrierLLM) Generate(ctx context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	var input struct {
		ActiveComponent string `json:"activeComponent"`
	}
	_ = json.Unmarshal(request.Input, &input)
	l.started <- input.ActiveComponent
	select {
	case <-l.release:
	case <-ctx.Done():
		return aiport.StoryResponse{}, ctx.Err()
	}
	return aiport.StoryResponse{Output: []byte(`{"summary":"checked","operations":[]}`)}, nil
}

func (l *setupDAGLLM) Identity() aiport.ProviderIdentity {
	return aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "google_gemini", Model: "dag-setup-test", Profile: "hosted"}
}

func (l *setupDAGLLM) Generate(ctx context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	var input struct {
		ActiveComponent string `json:"activeComponent"`
	}
	_ = json.Unmarshal(request.Input, &input)
	switch input.ActiveComponent {
	case string(setup.VisualBible):
		select {
		case <-l.visualStarted:
		default:
			close(l.visualStarted)
		}
		select {
		case <-l.visualRelease:
		case <-ctx.Done():
			return aiport.StoryResponse{}, ctx.Err()
		}
	case string(setup.OpeningSituation):
		l.openingOnce.Do(func() { close(l.openingStarted) })
	}
	return aiport.StoryResponse{Output: allResponse()}, nil
}

func (l *onlineSetupBarrierLLM) Identity() aiport.ProviderIdentity {
	return aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "google_gemini", Model: "parallel-setup-test", Profile: "hosted"}
}

func (l *onlineSetupBarrierLLM) Generate(ctx context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	var input struct {
		ActiveComponent string `json:"activeComponent"`
		Phase           string `json:"phase"`
	}
	_ = json.Unmarshal(request.Input, &input)
	l.mu.Lock()
	l.calls = append(l.calls, input.ActiveComponent+":"+input.Phase)
	l.mu.Unlock()
	if input.Phase == "component" && (input.ActiveComponent == string(setup.Player) || input.ActiveComponent == string(setup.World)) {
		l.started <- input.ActiveComponent
		select {
		case <-l.release:
		case <-ctx.Done():
			return aiport.StoryResponse{}, ctx.Err()
		}
	}
	return aiport.StoryResponse{Output: allResponse()}, nil
}

func (m *memRepo) CreateStory(_ context.Context, s story.Story, _ []string) error {
	m.story = s
	return nil
}
func (m *memRepo) GetStory(context.Context, story.ID) (story.Story, error) { return m.story, nil }
func (m *memRepo) ListStories(context.Context, story.UserID) ([]setuprepo.StoryOverview, error) {
	return m.stories, nil
}
func (m *memRepo) DeleteStory(_ context.Context, storyID story.ID, _ story.UserID) (bool, error) {
	m.deleted = storyID
	return true, nil
}
func (m *memRepo) ListComponents(context.Context, story.ID) ([]setup.Component, error) {
	o := []setup.Component{}
	for _, k := range setup.AllKeys {
		if v, ok := m.components[k]; ok {
			o = append(o, v)
		}
	}
	return o, nil
}
func (m *memRepo) UpsertComponents(_ context.Context, v []setup.Component) error {
	if m.components == nil {
		m.components = map[setup.ComponentKey]setup.Component{}
	}
	for _, x := range v {
		m.components[x.Key] = x
	}
	return nil
}
func (m *memRepo) SetComponent(_ context.Context, v setup.Component) error {
	m.components[v.Key] = v
	return nil
}
func (m *memRepo) CreateSetupGeneration(context.Context, event.GenerationID, story.ID, string, string, string) error {
	return nil
}
func (m *memRepo) FinishSetupGeneration(context.Context, event.GenerationID, string, *string) error {
	return nil
}
func (m *memRepo) StartStory(_ context.Context, _ story.ID, _ timeline.Timeline, x setuprepo.StartMaterialization) error {
	m.started = true
	m.start = x
	return nil
}
func allResponse() []byte {
	return []byte(`{
 "story_bible":{"premise":"A visitor arrives","tone":"mystery","themes":["trust"]},
 "player":{"name":"Alex","age":25,"description":"curious","goals":["understand"]},
 "world":{"name":"City","summary":"night","locations":[{"name":"apartment","description":"A quiet room"}]},
 "world_rules":{"systems":[{"id":"magic","name":"Magic","kind":"magic","description":"Magic converts focus into bounded effects.","resources":[{"id":"focus","name":"Focus","unit":"points","ownerScope":"hero","initialValue":5,"minValue":0,"maxValue":10}]}],"rules":[{"id":"MAGIC-001","systemId":"magic","title":"Focus cost","category":"cost","severity":"hard","statement":"Every spell consumes focus before producing an effect.","preconditions":["The caster can concentrate"],"costs":["At least one focus point"],"forbiddenResults":["A spell without a focus cost"],"exceptions":[],"tags":["magic","focus"],"visibility":"known_to_hero","status":"established"}],"glossary":[{"term":"Focus","definition":"A finite reserve used for magic."}]},
 "initial_cast":{"characters":[{"name":"Mira","age":27}]},
 "visual_bible":{"style":"manga","palette":"night"},
 "initial_quests":{"quests":[{"title":"Unmask the visitor","description":"Discover why the visitor came.","successCriteria":"The visitor's purpose is established.","stages":[{"kind":"task","title":"Speak to the visitor","description":"Open a cautious conversation.","successCriteria":"The visitor answers."},{"kind":"milestone","title":"Verify the visitor's story","description":"Find independent evidence.","successCriteria":"The story is confirmed or disproved."}]},{"title":"Protect the apartment","description":"Keep the home safe while the mystery unfolds.","successCriteria":"The immediate threat is neutralized.","stages":[{"kind":"event","title":"Identify the watcher","description":"Notice who is observing the building.","successCriteria":"The watcher is identified."},{"kind":"task","title":"Secure the entrance","description":"Reduce the immediate risk at the apartment.","successCriteria":"The entrance is secured."}]}]},
 "opening_situation":{"text":"Someone knocks at the door while rain taps against the window.\n\nAlex freezes and listens to the quiet hallway beyond the apartment.\n\nA shadow briefly crosses the narrow strip of light under the door.\n\nMira looks toward Alex but leaves the decision entirely to him.\n\nAlex steps close enough to hear the visitor breathing on the other side.","choices":["Open it","Ask who is there","Look through peephole","Stay silent"],"chapterTitle":"Night Visitor","chapterGoal":"Meet the visitor","sceneGoal":"Respond to the knock"}
}`)
}
func svc(repo *memRepo) *Service {
	llm := fakeai.NewScriptedStoryLLM(map[string][]byte{
		"setup_architect": allResponse(),
		"setup_editor":    []byte(`{"summary":"Уточнить героя","operations":[{"component":"player","operation":"set_field","path":"description","value":"наблюдательный и осторожный"},{"component":"player","operation":"set_field","path":"goals","value":["понять мотивы гостя","сохранить контроль над ситуацией"]}]}`),
	})
	owner := story.UserID(id.MustParse("00000000-0000-4000-8000-000000000001"))
	return &Service{Repo: repo, LLM: llm, Now: func() time.Time { return time.Unix(10, 0) }, OwnerID: owner}
}

func TestListReturnsResumeMetadata(t *testing.T) {
	owner := story.UserID(id.MustParse("00000000-0000-4000-8000-000000000001"))
	storyID := story.ID(id.MustParse("00000000-0000-4000-8000-000000000002"))
	timelineID := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000003"))
	updatedAt := time.Unix(20, 0)
	repo := &memRepo{stories: []setuprepo.StoryOverview{{
		Story:           story.Story{ID: storyID, OwnerID: owner, Title: "Ночная дверь", Description: "Незнакомец ждёт снаружи", Status: story.StatusActive, SemanticRevision: 1, CreatedAt: time.Unix(10, 0), UpdatedAt: time.Unix(11, 0)},
		ReadyComponents: 7,
		LatestTimeline:  &setuprepo.TimelineOverview{ID: timelineID, Name: "Основная линия", Status: timeline.StatusActive, HeadEventSeq: 14},
		LastActivityAt:  updatedAt,
	}}}

	items, err := svc(repo).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	got := items[0]
	if got.ID != storyID || got.TotalComponents != len(setup.AllKeys) || got.ReadyComponents != 7 {
		t.Fatalf("unexpected story metadata: %+v", got)
	}
	if got.LatestTimeline == nil || got.LatestTimeline.ID != timelineID || got.LatestTimeline.HeadEventSeq != 14 {
		t.Fatalf("unexpected timeline metadata: %+v", got.LatestTimeline)
	}
	if !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("updatedAt = %v, want %v", got.UpdatedAt, updatedAt)
	}
}

func TestDeleteUsesCurrentOwner(t *testing.T) {
	repo := &memRepo{}
	service := svc(repo)
	storyID := story.ID(id.MustParse("00000000-0000-4000-8000-000000000004"))
	if err := service.Delete(context.Background(), storyID); err != nil {
		t.Fatal(err)
	}
	if repo.deleted != storyID {
		t.Fatalf("deleted = %s, want %s", repo.deleted, storyID)
	}
}

func TestGenerateHonorsLockedComponents(t *testing.T) {
	repo := &memRepo{components: map[setup.ComponentKey]setup.Component{}}
	s := svc(repo)
	st, e := s.Create(context.Background(), CreateCommand{Title: "Night Visitor"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Generate(context.Background(), GenerateCommand{StoryID: st.ID})
	if e != nil {
		t.Fatal(e)
	}
	old := repo.components[setup.Player]
	old.Locked = true
	old.Payload = json.RawMessage(`{"name":"Locked Hero","age":30}`)
	repo.components[setup.Player] = old
	_, e = s.Generate(context.Background(), GenerateCommand{StoryID: st.ID, Keys: []setup.ComponentKey{setup.Player, setup.World}})
	if e != nil {
		t.Fatal(e)
	}
	if string(repo.components[setup.Player].Payload) != `{"name":"Locked Hero","age":30}` {
		t.Fatal("locked component regenerated")
	}
	if repo.components[setup.World].Revision != 2 {
		t.Fatal("unlocked component did not regenerate")
	}
}
func TestManualEditAndStartCreatesPlayableOpening(t *testing.T) {
	repo := &memRepo{components: map[setup.ComponentKey]setup.Component{}}
	s := svc(repo)
	st, _ := s.Create(context.Background(), CreateCommand{Title: "Story"})
	_, e := s.Generate(context.Background(), GenerateCommand{StoryID: st.ID})
	if e != nil {
		t.Fatal(e)
	}
	locked := true
	v, e := s.Edit(context.Background(), ManualEditCommand{StoryID: st.ID, Key: setup.StoryBible, Payload: json.RawMessage(`{"premise":"manual"}`), Locked: &locked})
	if e != nil {
		t.Fatal(e)
	}
	if v.Source != "manual" || !v.Locked || v.Revision != 2 {
		t.Fatal("manual edit metadata incorrect")
	}
	out, e := s.Start(context.Background(), st.ID)
	if e != nil {
		t.Fatal(e)
	}
	if !repo.started || out.Timeline.Name != "Main" || repo.start.OpeningText == "" || len(repo.start.Choices) != 4 {
		t.Fatal("story did not reach playable opening")
	}
	if len(repo.start.Quests) != 2 || len(repo.start.Quests[0].Stages) != 2 || len(repo.start.Quests[1].Stages) != 2 {
		t.Fatalf("multiple initial quests and stages were not materialized: %#v", repo.start.Quests)
	}
}

func TestAssistUpdatesOneNamedLocationWithoutReplacingCollection(t *testing.T) {
	repo := &memRepo{components: map[setup.ComponentKey]setup.Component{}}
	s := svc(repo)
	st, _ := s.Create(context.Background(), CreateCommand{Title: "Story"})
	if _, err := s.Generate(context.Background(), GenerateCommand{StoryID: st.ID}); err != nil {
		t.Fatal(err)
	}
	s.LLM = fakeai.NewScriptedStoryLLM(map[string][]byte{
		"setup_editor": []byte(`{"summary":"Уточнить одну локацию","operations":[{"component":"world","operation":"update_item","path":"world/locations/0","value":{"description":"A cramped apartment above the night market"}}]}`),
	})
	result, err := s.Assist(context.Background(), AssistCommand{
		StoryID: st.ID, Keys: []setup.ComponentKey{setup.World}, Instruction: "Измени только apartment",
		Action: "update_item", Target: AssistTarget{Path: "locations", MatchField: "name", MatchValue: "apartment"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 1 || len(result.Changes[0].Operations) != 1 {
		t.Fatalf("expected one addressable operation: %#v", result)
	}
	var after map[string]any
	if err = json.Unmarshal(result.Changes[0].After, &after); err != nil {
		t.Fatal(err)
	}
	locations := after["locations"].([]any)
	if len(locations) != 1 || locations[0].(map[string]any)["name"] != "apartment" || locations[0].(map[string]any)["description"] == "" {
		t.Fatalf("targeted update damaged the locations collection: %#v", locations)
	}
}

func TestDecodeAssistProposalAcceptsSingleOperationObject(t *testing.T) {
	proposal, err := decodeAssistProposal(json.RawMessage(`{"summary":"Уточнить стиль","operations":{"component":"visual_bible","operation":"set_field","path":"style","value":"cinematic"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Operations) != 1 || proposal.Operations[0].Path != "style" {
		t.Fatalf("unexpected normalized proposal: %#v", proposal)
	}
}

func TestNormalizeAddressableAssistUsesUITargetAndUnwrapsCollection(t *testing.T) {
	operations := []AssistOperation{{Component: setup.World, Operation: "create_item", Path: "initial_cast.characters", Value: json.RawMessage(`{"characters":[{"name":"Хагрид","role":"наставник"}]}`)}}
	normalized := normalizeAssistOperations(operations, setup.InitialCast, "add_item", AssistTarget{Path: "characters"})
	if len(normalized) != 1 || normalized[0].Component != setup.InitialCast || normalized[0].Operation != "add_item" || normalized[0].Path != "characters" {
		t.Fatalf("UI target was not made authoritative: %#v", normalized)
	}
	var character map[string]any
	if err := json.Unmarshal(normalized[0].Value, &character); err != nil {
		t.Fatal(err)
	}
	if character["name"] != "Хагрид" {
		t.Fatalf("wrapped character was not extracted: %#v", character)
	}
}

func TestCollectionDuplicateDetectionPreventsRepeatedCharacter(t *testing.T) {
	current := json.RawMessage(`{"characters":[{"name":"Рональд Хагрид","role":"Наставник"}]}`)
	candidate := json.RawMessage(`{"name":"Рональд Хагрид","role":"Защитник"}`)
	if !collectionAlreadyContains(current, "characters", candidate) {
		t.Fatal("same named character must be detected before append")
	}
}

func TestVisualAndOpeningQuickImprovementsAreSplitIntoFieldPlans(t *testing.T) {
	visual := assistPlans(setup.VisualBible, "improve_visual_style", AssistTarget{}, "Собери стиль", json.RawMessage(`{}`))
	if len(visual) != 3 || visual[0].Target.Path != "style" || visual[1].Target.Path != "palette" || visual[2].Target.Path != "cinematography" {
		t.Fatalf("visual improvement was not split by field: %#v", visual)
	}
	opening := assistPlans(setup.OpeningSituation, "improve_opening_prose", AssistTarget{}, "Усиль сцену", json.RawMessage(`{}`))
	if len(opening) != 1 || opening[0].Action != "set_field" || opening[0].Target.Path != "text" {
		t.Fatalf("opening prose improvement escaped text scope: %#v", opening)
	}
}

func TestAssistReviewAllRunsOneBoundedRequestPerComponent(t *testing.T) {
	repo := &memRepo{components: map[setup.ComponentKey]setup.Component{}}
	s := svc(repo)
	st, _ := s.Create(context.Background(), CreateCommand{Title: "Story"})
	if _, err := s.Generate(context.Background(), GenerateCommand{StoryID: st.ID}); err != nil {
		t.Fatal(err)
	}
	llm := fakeai.NewScriptedStoryLLM(map[string][]byte{
		"setup_editor": []byte(`{"summary":"Адресная проверка","operations":[{"component":"player","operation":"set_field","path":"reviewNote","value":"checked"}]}`),
	})
	s.LLM = llm
	result, err := s.Assist(context.Background(), AssistCommand{StoryID: st.ID, Instruction: "Проверь связи", Action: "review"})
	if err != nil {
		t.Fatal(err)
	}
	if len(llm.Requests) != len(setup.AllKeys) || len(result.Changes) != len(setup.AllKeys) {
		t.Fatalf("review was not split by component: requests=%d changes=%d", len(llm.Requests), len(result.Changes))
	}
	for index, request := range llm.Requests {
		var input struct {
			Requested []string `json:"requestedComponents"`
		}
		if err = json.Unmarshal(request.Input, &input); err != nil {
			t.Fatal(err)
		}
		if len(input.Requested) != 1 || input.Requested[0] != string(setup.AllKeys[index]) {
			t.Fatalf("request %d escaped section scope: %#v", index, input.Requested)
		}
	}
}

func TestAssistAddsStageToOneQuestWithoutReplacingSiblingStages(t *testing.T) {
	before := json.RawMessage(`{"quests":[{"title":"Find the crown","stages":[{"kind":"task","title":"Search the archive"}]}]}`)
	op := AssistOperation{Component: setup.InitialQuests, Operation: "add_child_item", Path: "quests", MatchField: "title", MatchValue: "Find the crown", ChildPath: "stages", Value: json.RawMessage(`{"kind":"event","title":"Survive the ambush"}`)}
	after, err := applyAssistOperation(before, op)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Quests []struct {
			Stages []map[string]any `json:"stages"`
		} `json:"quests"`
	}
	if err = json.Unmarshal(after, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Quests) != 1 || len(payload.Quests[0].Stages) != 2 || payload.Quests[0].Stages[0]["title"] != "Search the archive" {
		t.Fatalf("nested add replaced existing stages: %s", after)
	}
}

func TestInitialQuestGenerationNormalizesLocalModelArrayShape(t *testing.T) {
	raw := json.RawMessage(`[{"title":"Stop the storm","successCriteria":["Beacon active","Storm stopped"],"stages":[{"kind":"task","title":"Reach the lighthouse","successCriteria":["Door opened","Hero entered"]}]}]`)
	normalized, err := normalizeGeneratedComponent(setup.InitialQuests, raw)
	if err != nil {
		t.Fatal(err)
	}
	quests, err := parseInitialQuests(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if len(quests) != 1 || len(quests[0].Stages) != 1 || quests[0].SuccessCriteria != "Beacon active; Storm stopped" || quests[0].Stages[0].SuccessCriteria != "Door opened; Hero entered" {
		t.Fatalf("unexpected normalized quest hierarchy: %#v", quests)
	}
}

func TestInitialCastGenerationNormalizesLocalModelArrayShape(t *testing.T) {
	raw := json.RawMessage(`[{"name":"Гермиона Грейнджер","role":"ally"},{"name":"Рон Уизли","role":"ally"}]`)
	normalized, err := normalizeGeneratedComponent(setup.InitialCast, raw)
	if err != nil {
		t.Fatal(err)
	}
	var cast struct {
		Characters []map[string]any `json:"characters"`
	}
	if err = json.Unmarshal(normalized, &cast); err != nil {
		t.Fatal(err)
	}
	if len(cast.Characters) != 2 || cast.Characters[0]["name"] != "Гермиона Грейнджер" {
		t.Fatalf("unexpected normalized cast: %s", normalized)
	}
}

func TestOpeningSituationNormalizesObjectChoices(t *testing.T) {
	raw := json.RawMessage(`{"text":"A door opens.\n\nCold air enters.\n\nA lamp flickers.\n\nFootsteps stop.\n\nA choice must be made.","choices":[{"text":"Enter"},{"label":"Wait"},{"action":"Call out"},{"title":"Leave"}]}`)
	normalized, err := normalizeGeneratedComponent(setup.OpeningSituation, raw)
	if err != nil {
		t.Fatal(err)
	}
	var opening struct {
		Choices []string `json:"choices"`
	}
	if err = json.Unmarshal(normalized, &opening); err != nil {
		t.Fatal(err)
	}
	if len(opening.Choices) != 4 || opening.Choices[0] != "Enter" || opening.Choices[3] != "Leave" {
		t.Fatalf("unexpected normalized choices: %#v", opening.Choices)
	}
}

func TestOpeningSituationRejectsRepeatedParagraphs(t *testing.T) {
	repeated := "Алекс поднял сканер и увидел на экране три мерцающих узла, соединённых тонкими линиями энергии. Он остановился у дерева и отметил направление сигнала."
	raw, err := json.Marshal(map[string]any{
		"text":    strings.Join([]string{repeated, "Из оврага донёсся резкий крик, и проводница потребовала погасить экран.", repeated, "На тропе показалась вооружённая фигура, перекрывшая путь к руинам."}, "\n\n"),
		"choices": []string{"Спрятаться", "Заговорить", "Отступить", "Продолжить сканирование"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = normalizeGeneratedComponent(setup.OpeningSituation, raw); err == nil {
		t.Fatal("repeated opening paragraphs must be rejected")
	}
}

func TestGenerateRequestsSetupOneComponentAtATime(t *testing.T) {
	repo := &memRepo{components: map[setup.ComponentKey]setup.Component{}}
	s := svc(repo)
	llm := s.LLM.(*fakeai.ScriptedStoryLLM)
	st, err := s.Create(context.Background(), CreateCommand{Title: "Story"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Generate(context.Background(), GenerateCommand{StoryID: st.ID}); err != nil {
		t.Fatal(err)
	}
	expectedKeys := []setup.ComponentKey{setup.StoryBible, setup.Player, setup.World, setup.WorldRules, setup.WorldRules, setup.InitialCast, setup.VisualBible, setup.InitialQuests, setup.InitialQuests, setup.OpeningSituation, setup.OpeningSituation, setup.OpeningSituation}
	expectedPhases := []string{"component", "component", "component", "systems", "laws", "component", "component", "quest_outlines", "quest_stages", "blueprint", "prose_first", "prose_second"}
	expectedContextSizes := []int{0, 1, 2, 3, 4, 4, 5, 6, 7, 7, 8, 9}
	expectedTokenLimits := []int{1024, 1024, 1600, 1800, 3200, 1600, 1600, 1400, 2200, 1400, 1400, 1400}
	if len(llm.Requests) != len(expectedKeys) {
		t.Fatalf("expected %d component requests, got %d", len(expectedKeys), len(llm.Requests))
	}
	for index, request := range llm.Requests {
		var input struct {
			Components []string                   `json:"components"`
			Phase      string                     `json:"phase"`
			Canon      map[string]json.RawMessage `json:"canon"`
		}
		if err = json.Unmarshal(request.Input, &input); err != nil {
			t.Fatal(err)
		}
		if len(input.Components) != 1 || input.Components[0] != string(expectedKeys[index]) {
			t.Fatalf("request %d generated the wrong scope: %#v", index, input.Components)
		}
		if input.Phase != expectedPhases[index] {
			t.Fatalf("request %d used phase %q, want %q", index, input.Phase, expectedPhases[index])
		}
		if len(input.Canon) != expectedContextSizes[index] {
			t.Fatalf("request %d did not receive compact canon: %d components", index, len(input.Canon))
		}
		if request.MaxTokens != expectedTokenLimits[index] {
			t.Fatalf("request %d has unsafe token budget %d", index, request.MaxTokens)
		}
	}
}

func TestHostedSetupRunsOnlyIndependentDependencyWaveInParallel(t *testing.T) {
	repo := &memRepo{components: map[setup.ComponentKey]setup.Component{}}
	llm := &onlineSetupBarrierLLM{started: make(chan string, 2), release: make(chan struct{})}
	owner := story.UserID(id.MustParse("00000000-0000-4000-8000-000000000001"))
	service := &Service{Repo: repo, LLM: llm, Now: func() time.Time { return time.Unix(10, 0) }, OwnerID: owner}
	created, err := service.Create(context.Background(), CreateCommand{Title: "Parallel setup"})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, generateErr := service.Generate(context.Background(), GenerateCommand{StoryID: created.ID})
		result <- generateErr
	}()
	started := map[string]bool{}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for len(started) < 2 {
		select {
		case component := <-llm.started:
			started[component] = true
		case <-deadline.C:
			close(llm.release)
			t.Fatalf("player and world did not reach the same dependency wave: %#v", started)
		}
	}
	close(llm.release)
	select {
	case err = <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("hosted setup did not complete after the parallel wave was released")
	}
	if len(repo.components) != len(setup.AllKeys) {
		t.Fatalf("parallel setup lost components: got %d", len(repo.components))
	}
}

func TestHostedSetupOpeningDoesNotWaitForIndependentVisualBranch(t *testing.T) {
	repo := &memRepo{components: map[setup.ComponentKey]setup.Component{}}
	llm := &setupDAGLLM{visualStarted: make(chan struct{}), visualRelease: make(chan struct{}), openingStarted: make(chan struct{})}
	service := &Service{Repo: repo, LLM: llm, Now: func() time.Time { return time.Unix(10, 0) }, OwnerID: story.UserID(id.MustParse("00000000-0000-4000-8000-000000000001"))}
	created, err := service.Create(context.Background(), CreateCommand{Title: "DAG setup"})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, generateErr := service.Generate(context.Background(), GenerateCommand{StoryID: created.ID})
		result <- generateErr
	}()
	select {
	case <-llm.visualStarted:
	case <-time.After(time.Second):
		close(llm.visualRelease)
		t.Fatal("visual branch did not start")
	}
	select {
	case <-llm.openingStarted:
		// Opening depends on quests, not on visual style, so it should overlap
		// a slow visual branch instead of waiting at a wave barrier.
	case <-time.After(time.Second):
		close(llm.visualRelease)
		t.Fatal("opening was blocked by the independent visual branch")
	}
	close(llm.visualRelease)
	select {
	case err = <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("DAG setup did not complete")
	}
}

func TestHostedAssistantProcessesIndependentComponentsInParallel(t *testing.T) {
	storyID := story.ID(id.MustParse("00000000-0000-4000-8000-000000000071"))
	repo := &memRepo{
		story: story.Story{ID: storyID, Title: "Parallel assistant"},
		components: map[setup.ComponentKey]setup.Component{
			setup.StoryBible: {StoryID: storyID, Key: setup.StoryBible, Payload: json.RawMessage(`{"premise":"old"}`), Status: "ready"},
			setup.Player:     {StoryID: storyID, Key: setup.Player, Payload: json.RawMessage(`{"name":"Alex"}`), Status: "ready"},
		},
	}
	llm := &onlineAssistBarrierLLM{started: make(chan string, 2), release: make(chan struct{})}
	service := &Service{Repo: repo, LLM: llm, Now: time.Now}
	result := make(chan error, 1)
	go func() {
		_, assistErr := service.Assist(context.Background(), AssistCommand{StoryID: storyID, Instruction: "Проверь согласованность", Keys: []setup.ComponentKey{setup.StoryBible, setup.Player}})
		result <- assistErr
	}()
	started := map[string]bool{}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for len(started) < 2 {
		select {
		case component := <-llm.started:
			started[component] = true
		case <-deadline.C:
			close(llm.release)
			t.Fatalf("assistant components did not run concurrently: %#v", started)
		}
	}
	close(llm.release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("parallel assistant did not complete")
	}
}

type retrySetupLLM struct {
	calls int
}

func (l *retrySetupLLM) Identity() aiport.ProviderIdentity {
	return aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "fake", Model: "retry", Profile: "test"}
}

func (l *retrySetupLLM) Generate(context.Context, aiport.StoryRequest) (aiport.StoryResponse, error) {
	l.calls++
	if l.calls == 1 {
		return aiport.StoryResponse{}, aiport.ErrInvalidOutput
	}
	return aiport.StoryResponse{Output: allResponse()}, nil
}

func TestGenerateRetriesOneInvalidProviderResponse(t *testing.T) {
	repo := &memRepo{components: map[setup.ComponentKey]setup.Component{}}
	llm := &retrySetupLLM{}
	owner := story.UserID(id.MustParse("00000000-0000-4000-8000-000000000001"))
	s := &Service{Repo: repo, LLM: llm, Now: func() time.Time { return time.Unix(10, 0) }, OwnerID: owner}
	st, err := s.Create(context.Background(), CreateCommand{Title: "Story"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Generate(context.Background(), GenerateCommand{StoryID: st.ID}); err != nil {
		t.Fatal(err)
	}
	if llm.calls != 13 {
		t.Fatalf("expected exactly one retry, got %d calls", llm.calls)
	}
}

type failOpeningSetupLLM struct{ calls int }

func (l *failOpeningSetupLLM) Identity() aiport.ProviderIdentity {
	return aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "fake", Model: "partial", Profile: "test"}
}

func (l *failOpeningSetupLLM) Generate(_ context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	l.calls++
	var input struct {
		ActiveComponent string `json:"activeComponent"`
	}
	_ = json.Unmarshal(request.Input, &input)
	if input.ActiveComponent == string(setup.OpeningSituation) {
		return aiport.StoryResponse{}, aiport.ErrInvalidOutput
	}
	return aiport.StoryResponse{Output: allResponse()}, nil
}

func TestGeneratePersistsCompletedComponentsAndResumesOnlyMissingPhase(t *testing.T) {
	repo := &memRepo{components: map[setup.ComponentKey]setup.Component{}}
	llm := &failOpeningSetupLLM{}
	owner := story.UserID(id.MustParse("00000000-0000-4000-8000-000000000001"))
	s := &Service{Repo: repo, LLM: llm, Now: func() time.Time { return time.Unix(10, 0) }, OwnerID: owner}
	st, err := s.Create(context.Background(), CreateCommand{Title: "Story"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Generate(context.Background(), GenerateCommand{StoryID: st.ID}); !errors.Is(err, ErrIncompleteDraft) {
		t.Fatalf("expected opening failure, got %v", err)
	}
	if len(repo.components) != 7 {
		t.Fatalf("completed phases were not durable: got %d components", len(repo.components))
	}
	if _, exists := repo.components[setup.OpeningSituation]; exists {
		t.Fatal("failed opening must not be marked ready")
	}

	recovery := fakeai.NewScriptedStoryLLM(map[string][]byte{"setup_architect": allResponse()})
	s.LLM = recovery
	if _, err = s.Generate(context.Background(), GenerateCommand{StoryID: st.ID}); err != nil {
		t.Fatal(err)
	}
	if len(recovery.Requests) != 3 {
		t.Fatalf("resume regenerated completed components: got %d requests, want 3", len(recovery.Requests))
	}
	if len(repo.components) != len(setup.AllKeys) {
		t.Fatalf("resume did not finish setup: got %d components", len(repo.components))
	}
}

func TestCreateRejectsOnlyHardIdeaLimitAndGenerationCompactsSoftLimit(t *testing.T) {
	repo := &memRepo{components: map[setup.ComponentKey]setup.Component{}}
	s := svc(repo)
	if _, err := s.Create(context.Background(), CreateCommand{Title: "Too long", Idea: strings.Repeat("я", ideaHardLimitChars+1)}); !errors.Is(err, ErrIdeaTooLong) {
		t.Fatalf("expected hard idea limit, got %v", err)
	}
	st, err := s.Create(context.Background(), CreateCommand{Title: "Long but valid", Idea: strings.Repeat("сюжет ", 2500)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Generate(context.Background(), GenerateCommand{StoryID: st.ID}); err != nil {
		t.Fatal(err)
	}
	request := s.LLM.(*fakeai.ScriptedStoryLLM).Requests[0]
	var input struct {
		Idea struct {
			Text          string `json:"text"`
			Condensed     bool   `json:"condensed"`
			OriginalChars int    `json:"originalChars"`
		} `json:"idea"`
	}
	if err = json.Unmarshal(request.Input, &input); err != nil {
		t.Fatal(err)
	}
	if !input.Idea.Condensed || input.Idea.OriginalChars <= ideaSoftLimitChars || utf8.RuneCountInString(input.Idea.Text) > ideaSoftLimitChars {
		t.Fatalf("soft-limit compaction is not bounded: %+v", input.Idea)
	}
}

func TestGenerateStopsAfterBoundedInvalidProviderRetries(t *testing.T) {
	repo := &memRepo{components: map[setup.ComponentKey]setup.Component{}}
	s := svc(repo)
	llm := s.LLM.(*fakeai.ScriptedStoryLLM)
	llm.ErrByRole["setup_architect"] = aiport.ErrInvalidOutput
	st, err := s.Create(context.Background(), CreateCommand{Title: "Story"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Generate(context.Background(), GenerateCommand{StoryID: st.ID})
	if !errors.Is(err, ErrIncompleteDraft) {
		t.Fatalf("expected incomplete draft, got %v", err)
	}
	if len(llm.Requests) != setupComponentAttempts {
		t.Fatalf("expected %d bounded attempts, got %d", setupComponentAttempts, len(llm.Requests))
	}
}

type retryAssistLLM struct {
	calls int
}

func (l *retryAssistLLM) Identity() aiport.ProviderIdentity {
	return aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "fake", Model: "assist-retry", Profile: "test"}
}

func (l *retryAssistLLM) Generate(context.Context, aiport.StoryRequest) (aiport.StoryResponse, error) {
	l.calls++
	if l.calls == 1 {
		return aiport.StoryResponse{Output: []byte(`{"summary":"bad","operations":"not-an-array"}`)}, nil
	}
	return aiport.StoryResponse{Output: []byte(`{"summary":"Уточнить героя","operations":[{"component":"player","operation":"set_field","path":"description","value":"Наблюдательный и решительный"}]}`)}, nil
}

func TestAssistRetriesMalformedProposalForOnlySelectedComponent(t *testing.T) {
	llm := &retryAssistLLM{}
	current := json.RawMessage(`{"name":"Alex","description":"curious"}`)
	proposal, err := generateAssistProposal(context.Background(), llm, story.Story{Title: "Story"}, "Улучши героя", "improve", AssistTarget{}, setup.Player, nil, map[string]json.RawMessage{"player": current}, current)
	if err != nil {
		t.Fatal(err)
	}
	if llm.calls != 2 || len(proposal.Operations) != 1 {
		t.Fatalf("expected one scoped retry, calls=%d proposal=%#v", llm.calls, proposal)
	}
}
func TestStartRequiresAllReadyComponents(t *testing.T) {
	repo := &memRepo{components: map[setup.ComponentKey]setup.Component{}}
	s := svc(repo)
	st, _ := s.Create(context.Background(), CreateCommand{Title: "Story"})
	if _, e := s.Start(context.Background(), st.ID); e != setup.ErrNotReady {
		t.Fatalf("expected not ready: %v", e)
	}
}

func TestEnsureFourOpeningChoicesFillsAndDeduplicates(t *testing.T) {
	got := ensureFourOpeningChoices([]string{"Inspect", "Inspect", "Wait"})
	if len(got) != 4 {
		t.Fatalf("expected exactly four choices, got %d: %#v", len(got), got)
	}
	if got[0] != "Inspect" || got[1] != "Wait" {
		t.Fatalf("existing distinct choices should be preserved first: %#v", got)
	}
}

func TestAssistBuildsReviewableDiffWithoutMutatingCanonicalSetup(t *testing.T) {
	repo := &memRepo{components: map[setup.ComponentKey]setup.Component{}}
	s := svc(repo)
	st, _ := s.Create(context.Background(), CreateCommand{Title: "Story"})
	if _, err := s.Generate(context.Background(), GenerateCommand{StoryID: st.ID}); err != nil {
		t.Fatal(err)
	}
	canonical := append([]byte(nil), repo.components[setup.Player].Payload...)
	draft := json.RawMessage(`{"name":"Alex","age":25,"description":"несохранённый черновик","goals":["understand"],"advancedField":"keep-me"}`)

	result, err := s.Assist(context.Background(), AssistCommand{
		StoryID:     st.ID,
		Keys:        []setup.ComponentKey{setup.Player},
		Instruction: "Сделай героя более конкретным, не меняя имя и возраст",
		Drafts:      map[setup.ComponentKey]json.RawMessage{setup.Player: draft},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 1 || result.Changes[0].Key != setup.Player {
		t.Fatalf("unexpected assistant changes: %#v", result.Changes)
	}
	var after map[string]any
	if err = json.Unmarshal(result.Changes[0].After, &after); err != nil {
		t.Fatal(err)
	}
	if after["name"] != "Alex" || after["advancedField"] != "keep-me" {
		t.Fatalf("merge patch lost untouched fields: %#v", after)
	}
	if after["description"] != "наблюдательный и осторожный" {
		t.Fatalf("assistant patch was not applied to proposal: %#v", after)
	}
	if string(repo.components[setup.Player].Payload) != string(canonical) {
		t.Fatal("assistant must never mutate canonical setup before explicit user save")
	}
	foundDescription := false
	for _, path := range result.Changes[0].ChangedPaths {
		if path == "description" {
			foundDescription = true
		}
	}
	if !foundDescription {
		t.Fatalf("expected field-level diff path, got %#v", result.Changes[0].ChangedPaths)
	}
}

func TestAssistRefusesScopeContainingOnlyLockedComponent(t *testing.T) {
	repo := &memRepo{components: map[setup.ComponentKey]setup.Component{}}
	s := svc(repo)
	st, _ := s.Create(context.Background(), CreateCommand{Title: "Story"})
	if _, err := s.Generate(context.Background(), GenerateCommand{StoryID: st.ID}); err != nil {
		t.Fatal(err)
	}
	player := repo.components[setup.Player]
	player.Locked = true
	repo.components[setup.Player] = player
	_, err := s.Assist(context.Background(), AssistCommand{StoryID: st.ID, Keys: []setup.ComponentKey{setup.Player}, Instruction: "Улучши героя"})
	if err != ErrNoEditableComponents {
		t.Fatalf("expected ErrNoEditableComponents, got %v", err)
	}
}

func TestObjectMergePatchPreservesNestedUnknownFields(t *testing.T) {
	before := json.RawMessage(`{"known":"x","nested":{"keep":1,"change":2},"list":[1,2]}`)
	patch := json.RawMessage(`{"nested":{"change":3},"list":[9]}`)
	after, err := applyObjectMergePatch(before, patch)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err = json.Unmarshal(after, &got); err != nil {
		t.Fatal(err)
	}
	nested := got["nested"].(map[string]any)
	if nested["keep"] != float64(1) || nested["change"] != float64(3) {
		t.Fatalf("nested merge patch incorrect: %#v", nested)
	}
	if len(got["list"].([]any)) != 1 {
		t.Fatalf("arrays must be replaced as a whole: %#v", got["list"])
	}
}
