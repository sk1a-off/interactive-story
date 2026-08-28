package imagegeneration

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	cfg "github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/imagegeneration"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/imagegenerationrepo"
)

func xid(s string) id.ID { return id.MustParse("00000000-0000-4000-8000-" + s) }

type cfgRepo struct{ v cfg.Revision }

func (c cfgRepo) CreateRevision(context.Context, cfg.CreateRevisionCommand) (cfg.Revision, error) {
	return cfg.Revision{}, nil
}
func (c cfgRepo) Get(context.Context, id.ID) (cfg.Revision, error) { return c.v, nil }
func (c cfgRepo) List(context.Context) ([]cfg.Revision, error)     { return []cfg.Revision{c.v}, nil }
func (c cfgRepo) Active(context.Context) (cfg.Revision, error)     { return c.v, nil }
func (c cfgRepo) Activate(context.Context, id.ID) error            { return nil }

type upgradeCfgRepo struct {
	active    cfg.Revision
	created   cfg.Revision
	activated id.ID
}

func (r *upgradeCfgRepo) CreateRevision(_ context.Context, c cfg.CreateRevisionCommand) (cfg.Revision, error) {
	next, err := cfg.NewRevision(xid("000000000099"), r.active.Revision+1, c.StoryLLM, c.Embedding, c.Image)
	if err != nil {
		return cfg.Revision{}, err
	}
	next.Settings = c.Public.JSON()
	r.created = next
	return next, nil
}
func (r *upgradeCfgRepo) Get(context.Context, id.ID) (cfg.Revision, error) { return r.active, nil }
func (r *upgradeCfgRepo) List(context.Context) ([]cfg.Revision, error) {
	return []cfg.Revision{r.active}, nil
}
func (r *upgradeCfgRepo) Active(context.Context) (cfg.Revision, error) { return r.active, nil }
func (r *upgradeCfgRepo) Activate(_ context.Context, i id.ID) error    { r.activated = i; return nil }

type fakeRepo struct {
	view      domain.View
	src       repo.SceneSource
	sources   []repo.SceneSource
	createErr error
	selected  bool
}

func (f *fakeRepo) LoadSceneSource(context.Context, id.ID, id.ID) (repo.SceneSource, error) {
	if len(f.sources) > 0 {
		return f.sources[0], nil
	}
	return f.src, nil
}
func (f *fakeRepo) LoadSceneSources(context.Context, id.ID, id.ID) ([]repo.SceneSource, error) {
	if len(f.sources) > 0 {
		return f.sources, nil
	}
	return []repo.SceneSource{f.src}, nil
}
func (f *fakeRepo) LoadSceneSourceForBeat(_ context.Context, _, _ id.ID, beatID id.ID) (repo.SceneSource, error) {
	for _, source := range append(f.sources, f.src) {
		if source.BeatID == beatID {
			return source, nil
		}
	}
	return repo.SceneSource{}, errors.New("beat not found")
}
func (f *fakeRepo) HasAutomaticMoment(context.Context, id.ID, id.ID, int) (bool, error) {
	return false, nil
}
func (f *fakeRepo) Create(_ context.Context, g domain.Generation) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.view = domain.View{Generation: g, Images: []domain.Image{}}
	return nil
}

type recordingPromptBuilder struct {
	moments        map[id.ID][]ImagePrompt
	momentSources  []repo.SceneSource
	paragraphCalls []int
}

func (b *recordingPromptBuilder) Build(context.Context, repo.SceneSource, bool) (ImagePrompt, error) {
	return ImagePrompt{Positive: "cinematic scene", Negative: HardNegativePrompt, ShouldIllustrate: true, AnchorParagraph: 1}, nil
}
func (b *recordingPromptBuilder) BuildMoments(_ context.Context, source repo.SceneSource, _ bool) ([]ImagePrompt, error) {
	b.momentSources = append(b.momentSources, source)
	return b.moments[source.BeatID], nil
}
func (b *recordingPromptBuilder) BuildForParagraph(_ context.Context, _ repo.SceneSource, paragraph int) (ImagePrompt, error) {
	b.paragraphCalls = append(b.paragraphCalls, paragraph)
	return ImagePrompt{Positive: "selected paragraph cinematic scene", Negative: HardNegativePrompt, ShouldIllustrate: true, AnchorParagraph: paragraph}, nil
}
func (f *fakeRepo) Get(context.Context, id.ID) (domain.View, error) { return f.view, nil }
func (f *fakeRepo) ClaimNext(context.Context, string, time.Time, time.Duration) (domain.Generation, error) {
	return f.view.Generation, nil
}
func (f *fakeRepo) Complete(_ context.Context, _ id.ID, worker string, imgs []domain.Image, now time.Time) (domain.View, error) {
	f.view.Generation.Status = domain.Done
	f.view.Generation.LeaseOwner = ""
	f.view.Generation.FinishedAt = &now
	f.view.Images = imgs
	return f.view, nil
}
func (f *fakeRepo) Fail(context.Context, id.ID, string, time.Time, string, string) (domain.Generation, error) {
	f.view.Generation.Status = domain.Failed
	return f.view.Generation, nil
}
func (f *fakeRepo) Heartbeat(context.Context, string, *id.ID, time.Time) error { return nil }
func (f *fakeRepo) WorkerStatus(context.Context, time.Time) (repo.WorkerStatus, error) {
	return repo.WorkerStatus{}, nil
}
func (f *fakeRepo) SelectImage(context.Context, id.ID, id.ID) error { f.selected = true; return nil }

type memStorage struct{ calls int }

func (m *memStorage) Save(_ context.Context, key, ct string, data []byte) (string, error) {
	m.calls++
	return "/media/" + key, nil
}
func config() cfg.Revision {
	v, _ := cfg.NewRevision(xid("000000000001"), 1,
		ai.ProviderIdentity{Kind: ai.KindStoryLLM, Provider: "fake", Model: "s"},
		ai.ProviderIdentity{Kind: ai.KindEmbedding, Provider: "fake", Model: "e"},
		ai.ProviderIdentity{Kind: ai.KindImage, Provider: "perchance_browser", Model: "text-to-image-plugin", Profile: "digital-painting-768x768x2"})
	return v
}
func pngDataURL() string {
	raw := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("payload")...)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
}
func TestCreateForScenePinsPerchanceConfigAndSquareTwoImages(t *testing.T) {
	rp := &fakeRepo{src: repo.SceneSource{StoryID: xid("000000000010"), SceneID: xid("000000000011"), TimelineID: xid("000000000012"), BeatID: xid("000000000013"), Mood: "night", Goal: "door", Text: "A woman approaches a locked door."}}
	st := &memStorage{}
	svc := Service{Repo: rp, Configs: cfgRepo{config()}, Prompts: DeterministicPromptBuilder{}, Storage: st, Now: func() time.Time { return time.Unix(10, 0) }}
	v, e := svc.CreateForScene(context.Background(), CreateCommand{StoryID: rp.src.StoryID, SceneID: rp.src.SceneID, Trigger: domain.Automatic})
	if e != nil {
		t.Fatal(e)
	}
	g := v.Generation
	if g.Status != domain.Pending || g.Width != 768 || g.Height != 768 || g.ImageCount != 2 || g.Style != "digital-painting" {
		t.Fatalf("wrong generation defaults: %#v", g)
	}
	if g.StylePrompt == "" || g.StyleNegativePrompt == "" {
		t.Fatal("Digital Painting style prompt provenance is missing")
	}
	if g.Provider.Provider != "perchance_browser" || g.SourceBeatID == nil {
		t.Fatal("provider/beat provenance missing")
	}
}

func TestCreateForSceneUpgradesLegacyLocalImageProvider(t *testing.T) {
	legacy := config()
	legacy.Image = ai.ProviderIdentity{Kind: ai.KindImage, Provider: "manual", Model: "manual-v1"}
	legacy.Settings = []byte(`{"storyLlmContext":16384,"storyLlmKvDType":"q8_0"}`)
	configs := &upgradeCfgRepo{active: legacy}
	rp := &fakeRepo{src: repo.SceneSource{StoryID: xid("000000000030"), SceneID: xid("000000000031"), TimelineID: xid("000000000032"), BeatID: xid("000000000033"), Text: "A quiet corridor."}}
	svc := Service{Repo: rp, Configs: configs, Prompts: DeterministicPromptBuilder{}, Now: func() time.Time { return time.Unix(30, 0) }}
	v, err := svc.CreateForScene(context.Background(), CreateCommand{StoryID: rp.src.StoryID, SceneID: rp.src.SceneID, Trigger: domain.Manual})
	if err != nil {
		t.Fatal(err)
	}
	if configs.created.Image.Provider != "perchance_browser" || configs.created.Image.Model != "text-to-image-plugin" {
		t.Fatalf("legacy provider was not upgraded: %#v", configs.created.Image)
	}
	if configs.activated != configs.created.ID {
		t.Fatal("new provider revision was not activated")
	}
	if v.Generation.ConfigRevisionID != configs.created.ID || v.Generation.Provider.Provider != "perchance_browser" {
		t.Fatal("image job did not pin upgraded provider revision")
	}
}

func TestCompleteRequiresExactlyTwoAndIsIdempotent(t *testing.T) {
	rp := &fakeRepo{}
	now := time.Unix(20, 0)
	rp.view.Generation = domain.Generation{ID: xid("000000000020"), StoryID: xid("000000000021"), SceneID: xid("000000000022"), Status: domain.Running, LeaseOwner: "browser-local-1"}
	st := &memStorage{}
	svc := Service{Repo: rp, Storage: st, Now: func() time.Time { return now }}
	if _, e := svc.Complete(context.Background(), rp.view.Generation.ID, "browser-local-1", WorkerResult{Images: []string{pngDataURL()}}); e != ErrWrongImageCount {
		t.Fatalf("want count error: %v", e)
	}
	v, e := svc.Complete(context.Background(), rp.view.Generation.ID, "browser-local-1", WorkerResult{Images: []string{pngDataURL(), pngDataURL()}})
	if e != nil {
		t.Fatal(e)
	}
	if v.Generation.Status != domain.Done || len(v.Images) != 2 || st.calls != 2 {
		t.Fatal("completion failed")
	}
	if _, e = svc.Complete(context.Background(), rp.view.Generation.ID, "different-worker", WorkerResult{Images: []string{"bad", "bad"}}); e != nil {
		t.Fatal("done result must be idempotent")
	}
	if st.calls != 2 {
		t.Fatal("idempotent retry wrote files again")
	}
}
func TestDecodeRejectsMimeSpoof(t *testing.T) {
	fake := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("not-a-jpeg"))
	if _, e := DecodeDataURL(fake); e == nil {
		t.Fatal("mime spoof accepted")
	}
}

var _ = story.ID("")
var _ = timeline.ID("")

func TestScheduleBeatForcesIllustrationMoment(t *testing.T) {
	rp := &fakeRepo{src: repo.SceneSource{
		StoryID: xid("000000000070"), SceneID: xid("000000000071"),
		TimelineID: xid("000000000072"), BeatID: xid("000000000073"),
		Text: "Над древней башней раскрылся гигантский светящийся парус, и его золотые отблески пробежали по мокрым крышам, лицам стражников и каменным статуям площади.",
	}}
	svc := Service{
		Repo: rp, Configs: cfgRepo{config()}, Prompts: DeterministicPromptBuilder{},
		Now: func() time.Time { return time.Unix(70, 0) },
	}
	gid, err := svc.ScheduleBeat(context.Background(), rp.src.StoryID, rp.src.SceneID)
	if err != nil {
		t.Fatal(err)
	}
	if gid.IsZero() || rp.view.Generation.SourceBeatID == nil || *rp.view.Generation.SourceBeatID != rp.src.BeatID {
		t.Fatal("follow-up beat did not receive its own illustration generation")
	}
}

func TestDurablePromptJobUsesItsCommittedBeatInsteadOfLatestSceneBeat(t *testing.T) {
	storyID, sceneID := xid("000000000074"), xid("000000000075")
	older := repo.SceneSource{StoryID: storyID, SceneID: sceneID, BeatID: xid("000000000076"), BeatPosition: 1, Text: "Над древней площадью медленно раскрылся огромный светящийся парус, отражаясь золотыми полосами в мокром камне, тёмных окнах башни и удивлённых лицах замерших стражников."}
	latest := repo.SceneSource{StoryID: storyID, SceneID: sceneID, BeatID: xid("000000000077"), BeatPosition: 2, Text: "Позднее герой вошёл в башню и закрыл за собой тяжёлую дверь."}
	rp := &fakeRepo{sources: []repo.SceneSource{latest, older}}
	builder := &recordingPromptBuilder{moments: map[id.ID][]ImagePrompt{
		older.BeatID: {{Positive: "cinematic luminous sail above a wet stone square with guards", Negative: HardNegativePrompt, ShouldIllustrate: true, AnchorParagraph: 1}},
	}}
	svc := Service{Repo: rp, Configs: cfgRepo{config()}, Prompts: builder, Now: func() time.Time { return time.Unix(74, 0) }}
	err := svc.SchedulePromptJob(context.Background(), repo.PromptJob{StoryID: storyID, SceneID: sceneID, SourceBeatID: older.BeatID, ForceIllustration: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(builder.momentSources) != 1 || builder.momentSources[0].BeatID != older.BeatID {
		t.Fatalf("durable job drifted to another beat: %#v", builder.momentSources)
	}
	if rp.view.Generation.SourceBeatID == nil || *rp.view.Generation.SourceBeatID != older.BeatID {
		t.Fatal("image generation lost durable source beat")
	}
}

func TestParagraphCanIllustrateRejectsDialogueAndShortConnectors(t *testing.T) {
	dialogue := "— Я видел огни за перевалом и точно знаю, что караван уже близко, — тихо сказал проводник, не сводя глаз с тёмной дороги."
	connector := "Через несколько минут они продолжили путь."
	description := "За разбитым окном медленно поднимался огромный механический маяк, и холодный синий свет скользил по лицам замерших путников и мокрым камням площади."
	if ParagraphCanIllustrate(dialogue) {
		t.Fatal("a dialogue paragraph must not expose an illustration action")
	}
	if ParagraphCanIllustrate(connector) {
		t.Fatal("a short connective paragraph must not expose an illustration action")
	}
	if !ParagraphCanIllustrate(description) {
		t.Fatal("a substantial visual description should be illustratable")
	}
}

func TestCreateNextForSceneSkipsOccupiedMomentAndSearchesOlderBeat(t *testing.T) {
	storyID, sceneID := xid("000000000080"), xid("000000000081")
	newBeat := repo.SceneSource{
		StoryID: storyID, SceneID: sceneID, BeatID: xid("000000000082"), BeatPosition: 2,
		Text: strings.Join([]string{
			"Короткий переход.",
			"— Мы должны идти сейчас, пока ворота ещё открыты и стража не заметила нас.",
			"Они кивнули.",
			"Наступила ночь.",
		}, "\n\n"),
	}
	oldBeat := repo.SceneSource{
		StoryID: storyID, SceneID: sceneID, BeatID: xid("000000000083"), BeatPosition: 1,
		ExcludedParagraphs: []int{1},
		Text: strings.Join([]string{
			"На площади уже стояла закрытая карета, освещённая десятками красных фонарей, а вокруг неё неподвижно замерли люди в серебряных масках.",
			"Над крышами медленно раскрылось гигантское механическое крыло, заслонив луну и осыпав площадь искрами, от которых толпа одновременно отступила к стенам.",
			"Путники остановились.",
			"Ветер стих.",
		}, "\n\n"),
	}
	rp := &fakeRepo{sources: []repo.SceneSource{newBeat, oldBeat}}
	builder := &recordingPromptBuilder{moments: map[id.ID][]ImagePrompt{
		oldBeat.BeatID: {
			{Positive: "occupied carriage moment", Negative: HardNegativePrompt, ShouldIllustrate: true, AnchorParagraph: 1},
			{Positive: "mechanical wing over moonlit square", Negative: HardNegativePrompt, ShouldIllustrate: true, AnchorParagraph: 2},
		},
	}}
	svc := Service{Repo: rp, Configs: cfgRepo{config()}, Prompts: builder, Now: func() time.Time { return time.Unix(80, 0) }}
	view, err := svc.CreateNextForScene(context.Background(), storyID, sceneID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Generation.SourceBeatID == nil || *view.Generation.SourceBeatID != oldBeat.BeatID || view.Generation.AnchorParagraph != 2 {
		t.Fatalf("expected unoccupied paragraph 2 from older beat, got %#v", view.Generation)
	}
	if len(builder.momentSources) != 1 || !containsInt(builder.momentSources[0].ExcludedParagraphs, 1) {
		t.Fatalf("visual director did not receive occupied anchors: %#v", builder.momentSources)
	}
}

func TestCreateForParagraphBuildsPromptOnlyForSelectedAnchor(t *testing.T) {
	storyID, sceneID, beatID := xid("000000000090"), xid("000000000091"), xid("000000000092")
	source := repo.SceneSource{StoryID: storyID, SceneID: sceneID, BeatID: beatID, Text: strings.Join([]string{
		"Он вошёл.",
		"Под стеклянным куполом вспыхнуло искусственное солнце, осветив тысячи подвешенных мостов и одинокую фигуру в белом плаще на самой высокой башне.",
		"— Кто здесь?",
		"Ответа не было.",
	}, "\n\n")}
	rp := &fakeRepo{src: source}
	builder := &recordingPromptBuilder{}
	svc := Service{Repo: rp, Configs: cfgRepo{config()}, Prompts: builder, Now: func() time.Time { return time.Unix(90, 0) }}
	view, err := svc.CreateForParagraph(context.Background(), ParagraphCommand{StoryID: storyID, SceneID: sceneID, BeatID: beatID, Paragraph: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(builder.paragraphCalls) != 1 || builder.paragraphCalls[0] != 2 || view.Generation.AnchorParagraph != 2 {
		t.Fatalf("prompt was not built exactly on demand for paragraph 2: calls=%v generation=%#v", builder.paragraphCalls, view.Generation)
	}
	if _, err = svc.CreateForParagraph(context.Background(), ParagraphCommand{StoryID: storyID, SceneID: sceneID, BeatID: beatID, Paragraph: 3}); !errors.Is(err, ErrParagraphNotIllustratable) {
		t.Fatalf("dialogue paragraph must be rejected before prompt generation, got %v", err)
	}
	if len(builder.paragraphCalls) != 1 {
		t.Fatal("a prompt was generated for a rejected dialogue paragraph")
	}
}

func TestCreateForParagraphMapsConcurrentAnchorConflict(t *testing.T) {
	storyID, sceneID, beatID := xid("000000000093"), xid("000000000094"), xid("000000000095")
	source := repo.SceneSource{StoryID: storyID, SceneID: sceneID, BeatID: beatID, Text: "В центре опустевшей станции раскрылся сияющий портал, отражаясь в мокром полу и силуэтах трёх осторожно приближающихся исследователей."}
	rp := &fakeRepo{src: source, createErr: repo.ErrAnchorOccupied}
	svc := Service{Repo: rp, Configs: cfgRepo{config()}, Prompts: &recordingPromptBuilder{}, Now: func() time.Time { return time.Unix(95, 0) }}
	_, err := svc.CreateForParagraph(context.Background(), ParagraphCommand{StoryID: storyID, SceneID: sceneID, BeatID: beatID, Paragraph: 1})
	if !errors.Is(err, ErrIllustrationExists) {
		t.Fatalf("expected a stable duplicate error, got %v", err)
	}
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
