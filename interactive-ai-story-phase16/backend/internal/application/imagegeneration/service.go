package imagegeneration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/imagegeneration"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/aiconfigrepo"
	"github.com/local/interactive-ai-story/backend/internal/ports/imagegenerationrepo"
	"github.com/local/interactive-ai-story/backend/internal/ports/imagestorage"
)

var (
	ErrWrongImageCount           = errors.New("worker must return exactly two images")
	ErrWorkerMismatch            = errors.New("worker does not own image generation lease")
	ErrUnsupportedProvider       = errors.New("active image provider is not perchance_browser")
	ErrIllustrationNotNeeded     = errors.New("visual director decided this beat does not need an illustration")
	ErrIllustrationLimit         = errors.New("automatic illustration limit reached for this scene")
	ErrIllustrationExists        = errors.New("automatic illustration already exists for this beat")
	ErrNoIllustrationCandidate   = errors.New("no unillustrated visual paragraph remains in this scene")
	ErrParagraphNotIllustratable = errors.New("selected paragraph is not suitable for an illustration")
	ErrParagraphOutOfRange       = errors.New("selected paragraph does not exist")
)

const (
	DefaultLeaseTimeout = 5 * time.Minute
)

type Service struct {
	Repo         imagegenerationrepo.Repository
	Configs      aiconfigrepo.Repository
	Prompts      SceneImagePromptBuilder
	Storage      imagestorage.Storage
	Now          func() time.Time
	LeaseTimeout time.Duration
	Logger       *slog.Logger
}
type CreateCommand struct {
	StoryID, SceneID id.ID
	Trigger          domain.Trigger
	Force            bool
	MomentIndex      int
}
type ParagraphCommand struct {
	StoryID, SceneID, BeatID id.ID
	Paragraph                int
}
type WorkerResult struct{ Images []string }
type WorkerFailure struct{ ErrorCode, Detail string }

func (s Service) CreateForScene(ctx context.Context, c CreateCommand) (out domain.View, err error) {
	defer func() {
		if err != nil && s.Logger != nil {
			s.Logger.Warn("image generation create failed", "story_id", c.StoryID, "scene_id", c.SceneID, "trigger", c.Trigger, "error", err)
		}
	}()
	if s.Repo == nil || s.Configs == nil || s.Prompts == nil {
		return domain.View{}, errors.New("image generation service unavailable")
	}
	src, e := s.Repo.LoadSceneSource(ctx, c.StoryID, c.SceneID)
	if e != nil {
		return domain.View{}, e
	}
	momentIndex := c.MomentIndex
	if momentIndex < 1 {
		momentIndex = 1
	}
	if c.Trigger == domain.Automatic && !src.BeatID.IsZero() {
		exists, existsErr := s.Repo.HasAutomaticMoment(ctx, c.SceneID, src.BeatID, momentIndex)
		if existsErr != nil {
			return domain.View{}, existsErr
		}
		if exists {
			return domain.View{}, ErrIllustrationExists
		}
	}
	prompt, e := s.Prompts.Build(ctx, src, c.Force || c.Trigger == domain.Manual)
	if e != nil {
		return domain.View{}, e
	}
	if c.Trigger == domain.Automatic && !c.Force && !prompt.ShouldIllustrate {
		return domain.View{}, ErrIllustrationNotNeeded
	}
	cfg, e := s.activePerchanceConfig(ctx)
	if e != nil {
		return domain.View{}, e
	}
	gid, e := id.New()
	if e != nil {
		return domain.View{}, e
	}
	now := s.now()
	beat := src.BeatID
	if beat.IsZero() {
		beat = ""
	}
	var beatPtr *id.ID
	if !beat.IsZero() {
		beatPtr = &beat
	}
	g, e := domain.New(gid, c.StoryID, c.SceneID, cfg.ID, beatPtr, cfg.Image, prompt.Positive, prompt.Negative, c.Trigger, now)
	if e != nil {
		return domain.View{}, e
	}
	g.MomentIndex = momentIndex
	g.AnchorParagraph = prompt.AnchorParagraph
	g.PromptSetRevisionID = prompt.PromptSetRevisionID
	if e = s.Repo.Create(ctx, g); e != nil {
		return domain.View{}, e
	}
	return domain.View{Generation: g, Images: []domain.Image{}}, nil
}

func (s Service) activePerchanceConfig(ctx context.Context) (aiconfig.Revision, error) {
	cfg, err := s.Configs.Active(ctx)
	if err != nil {
		return aiconfig.Revision{}, err
	}
	if cfg.Image.Provider == "perchance_browser" {
		return cfg, nil
	}

	// Phase 16 replaces the old local placeholder image providers with the
	// browser Perchance worker. A database can legitimately still have one of
	// those revisions active (for example when it was created before the image
	// migration). Preserve immutable provenance by creating a NEW revision and
	// activating it for future jobs rather than mutating the old revision.
	if cfg.Image.Provider != "manual" && cfg.Image.Provider != "fake" {
		return aiconfig.Revision{}, fmt.Errorf("%w: %s", ErrUnsupportedProvider, cfg.Image.Provider)
	}

	var public aiconfig.PublicSettings
	if len(cfg.Settings) != 0 {
		_ = json.Unmarshal(cfg.Settings, &public)
	}
	if public.StoryLLMContext == 0 {
		public.StoryLLMContext = 16384
	}
	if public.StoryLLMKVDType == "" {
		public.StoryLLMKVDType = "q8_0"
	}

	next, err := s.Configs.CreateRevision(ctx, aiconfig.CreateRevisionCommand{
		StoryLLM:  cfg.StoryLLM,
		Embedding: cfg.Embedding,
		Image: ai.ProviderIdentity{
			Kind:     ai.KindImage,
			Provider: "perchance_browser",
			Model:    "text-to-image-plugin",
			Profile:  "digital-painting-768x768x2",
		},
		Public: public,
	})
	if err != nil {
		return aiconfig.Revision{}, fmt.Errorf("upgrade legacy image provider: %w", err)
	}
	if err = s.Configs.Activate(ctx, next.ID); err != nil {
		return aiconfig.Revision{}, fmt.Errorf("activate Perchance image provider: %w", err)
	}
	if s.Logger != nil {
		s.Logger.Info("legacy image provider upgraded", "old_provider", cfg.Image.Provider, "config_revision_id", next.ID, "provider", next.Image.Provider)
	}
	return next, nil
}

func (s Service) Get(ctx context.Context, gid id.ID) (domain.View, error) {
	return s.Repo.Get(ctx, gid)
}
func (s Service) ClaimNext(ctx context.Context, worker string) (domain.Generation, error) {
	ttl := s.LeaseTimeout
	if ttl <= 0 {
		ttl = DefaultLeaseTimeout
	}
	return s.Repo.ClaimNext(ctx, worker, s.now(), ttl)
}
func (s Service) Complete(ctx context.Context, gid id.ID, worker string, input WorkerResult) (domain.View, error) {
	current, e := s.Repo.Get(ctx, gid)
	if e != nil {
		return domain.View{}, e
	}
	if current.Generation.Status == domain.Done {
		return current, nil
	}
	if current.Generation.Status != domain.Running || current.Generation.LeaseOwner != worker {
		return domain.View{}, ErrWorkerMismatch
	}
	if len(input.Images) != domain.DefaultCount {
		return domain.View{}, ErrWrongImageCount
	}
	if s.Storage == nil {
		return domain.View{}, errors.New("image storage unavailable")
	}
	imgs := make([]domain.Image, 0, 2)
	for i, dataURL := range input.Images {
		decoded, e := DecodeDataURL(dataURL)
		if e != nil {
			return domain.View{}, e
		}
		iid, e := id.New()
		if e != nil {
			return domain.View{}, e
		}
		key := fmt.Sprintf("stories/%s/scenes/%s/%s/%d%s", current.Generation.StoryID, current.Generation.SceneID, gid, i+1, decoded.Extension)
		url, e := s.Storage.Save(ctx, key, decoded.ContentType, decoded.Data)
		if e != nil {
			return domain.View{}, e
		}
		imgs = append(imgs, domain.Image{ID: iid, StoryID: current.Generation.StoryID, SceneID: current.Generation.SceneID, GenerationID: gid, Variant: i + 1, URL: url, ContentType: decoded.ContentType, ByteSize: int64(len(decoded.Data)), CreatedAt: s.now()})
	}
	return s.Repo.Complete(ctx, gid, worker, imgs, s.now())
}
func (s Service) Fail(ctx context.Context, gid id.ID, worker string, f WorkerFailure) (domain.Generation, error) {
	return s.Repo.Fail(ctx, gid, worker, s.now(), f.ErrorCode, f.Detail)
}
func (s Service) Heartbeat(ctx context.Context, worker string, active *id.ID) error {
	return s.Repo.Heartbeat(ctx, worker, active, s.now())
}
func (s Service) WorkerStatus(ctx context.Context) (imagegenerationrepo.WorkerStatus, error) {
	return s.Repo.WorkerStatus(ctx, s.now())
}
func (s Service) SelectImage(ctx context.Context, sceneID, imageID id.ID) error {
	return s.Repo.SelectImage(ctx, sceneID, imageID)
}

func ParagraphCanIllustrate(text string) bool {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) < 90 || len(strings.Fields(text)) < 14 {
		return false
	}
	startsDialogue := strings.HasPrefix(text, "—") || strings.HasPrefix(text, "-") || strings.HasPrefix(text, "«") || strings.HasPrefix(text, "\"")
	if startsDialogue {
		return false
	}
	return true
}

func excludeUnillustratableParagraphs(source imagegenerationrepo.SceneSource) (imagegenerationrepo.SceneSource, bool) {
	paragraphs := narrative.DisplayParagraphs(source.Text)
	excluded := make(map[int]struct{}, len(source.ExcludedParagraphs))
	for _, anchor := range source.ExcludedParagraphs {
		if anchor >= 1 && anchor <= len(paragraphs) {
			excluded[anchor] = struct{}{}
		}
	}
	for index, paragraph := range paragraphs {
		if !ParagraphCanIllustrate(paragraph) {
			excluded[index+1] = struct{}{}
		}
	}
	hasCandidate := false
	for index := range paragraphs {
		if _, blocked := excluded[index+1]; !blocked {
			hasCandidate = true
			break
		}
	}
	source.ExcludedParagraphs = source.ExcludedParagraphs[:0]
	for anchor := range excluded {
		source.ExcludedParagraphs = append(source.ExcludedParagraphs, anchor)
	}
	sort.Ints(source.ExcludedParagraphs)
	return source, hasCandidate
}

func (s Service) CreateNextForScene(ctx context.Context, storyID, sceneID id.ID) (domain.View, error) {
	if s.Repo == nil || s.Prompts == nil {
		return domain.View{}, errors.New("image generation service unavailable")
	}
	sources, err := s.Repo.LoadSceneSources(ctx, storyID, sceneID)
	if err != nil {
		return domain.View{}, err
	}
	builder, ok := s.Prompts.(SceneImageMomentPromptBuilder)
	if !ok {
		return domain.View{}, errors.New("visual moment selector unavailable")
	}
	for _, source := range sources {
		source, hasCandidate := excludeUnillustratableParagraphs(source)
		if !hasCandidate {
			continue
		}
		moments, buildErr := builder.BuildMoments(ctx, source, false)
		if buildErr != nil {
			return domain.View{}, buildErr
		}
		for _, moment := range moments {
			if containsParagraph(source.ExcludedParagraphs, moment.AnchorParagraph) {
				continue
			}
			view, createErr := s.createPromptGeneration(ctx, storyID, sceneID, source, moment, domain.Manual, 1)
			if errors.Is(createErr, imagegenerationrepo.ErrAnchorOccupied) {
				continue
			}
			return view, createErr
		}
	}
	return domain.View{}, ErrNoIllustrationCandidate
}

func containsParagraph(values []int, paragraph int) bool {
	index := sort.SearchInts(values, paragraph)
	return index < len(values) && values[index] == paragraph
}

func (s Service) CreateForParagraph(ctx context.Context, c ParagraphCommand) (domain.View, error) {
	if c.Paragraph < 1 {
		return domain.View{}, ErrParagraphOutOfRange
	}
	source, err := s.Repo.LoadSceneSourceForBeat(ctx, c.StoryID, c.SceneID, c.BeatID)
	if err != nil {
		return domain.View{}, err
	}
	paragraphs := narrative.DisplayParagraphs(source.Text)
	if c.Paragraph > len(paragraphs) {
		return domain.View{}, ErrParagraphOutOfRange
	}
	if !ParagraphCanIllustrate(paragraphs[c.Paragraph-1]) {
		return domain.View{}, ErrParagraphNotIllustratable
	}
	for _, anchor := range source.ExcludedParagraphs {
		if anchor == c.Paragraph {
			return domain.View{}, ErrIllustrationExists
		}
	}
	var prompt ImagePrompt
	if builder, ok := s.Prompts.(SceneParagraphPromptBuilder); ok {
		prompt, err = builder.BuildForParagraph(ctx, source, c.Paragraph)
	} else {
		prompt, err = s.Prompts.Build(ctx, source, true)
		prompt.AnchorParagraph = c.Paragraph
	}
	if err != nil {
		return domain.View{}, err
	}
	view, err := s.createPromptGeneration(ctx, c.StoryID, c.SceneID, source, prompt, domain.Manual, 1)
	if errors.Is(err, imagegenerationrepo.ErrAnchorOccupied) {
		return domain.View{}, ErrIllustrationExists
	}
	return view, err
}

func (s Service) createPromptGeneration(ctx context.Context, storyID, sceneID id.ID, source imagegenerationrepo.SceneSource, prompt ImagePrompt, trigger domain.Trigger, momentIndex int) (domain.View, error) {
	cfg, err := s.activePerchanceConfig(ctx)
	if err != nil {
		return domain.View{}, err
	}
	gid, err := id.New()
	if err != nil {
		return domain.View{}, err
	}
	beat := source.BeatID
	g, err := domain.New(gid, storyID, sceneID, cfg.ID, &beat, cfg.Image, prompt.Positive, prompt.Negative, trigger, s.now())
	if err != nil {
		return domain.View{}, err
	}
	g.MomentIndex = momentIndex
	g.AnchorParagraph = prompt.AnchorParagraph
	g.PromptSetRevisionID = prompt.PromptSetRevisionID
	if err = s.Repo.Create(ctx, g); err != nil {
		return domain.View{}, err
	}
	return domain.View{Generation: g, Images: []domain.Image{}}, nil
}
func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s Service) scheduleAutomaticMoments(ctx context.Context, storyID, sceneID, sourceBeatID id.ID, forceAtLeastOne bool) ([]id.ID, error) {
	if s.Repo == nil || s.Prompts == nil {
		return nil, errors.New("image generation service unavailable")
	}
	var src imagegenerationrepo.SceneSource
	var err error
	if sourceBeatID.IsZero() {
		src, err = s.Repo.LoadSceneSource(ctx, storyID, sceneID)
	} else {
		src, err = s.Repo.LoadSceneSourceForBeat(ctx, storyID, sceneID, sourceBeatID)
	}
	if err != nil {
		return nil, err
	}
	src, hasCandidate := excludeUnillustratableParagraphs(src)
	if !hasCandidate {
		return nil, ErrIllustrationNotNeeded
	}
	builder, ok := s.Prompts.(SceneImageMomentPromptBuilder)
	if !ok {
		// Compatibility fallback for alternate/test builders.
		one, buildErr := s.Prompts.Build(ctx, src, forceAtLeastOne)
		if buildErr != nil {
			return nil, buildErr
		}
		if !forceAtLeastOne && !one.ShouldIllustrate {
			return nil, ErrIllustrationNotNeeded
		}
		builder = singleMomentBuilder{Prompt: one}
	}
	moments, err := builder.BuildMoments(ctx, src, forceAtLeastOne)
	if err != nil {
		return nil, err
	}
	if len(moments) == 0 {
		return nil, ErrIllustrationNotNeeded
	}
	if len(moments) > 2 {
		moments = moments[:2]
	}

	created := make([]id.ID, 0, len(moments))
	for i, moment := range moments {
		momentIndex := i + 1
		if !src.BeatID.IsZero() {
			exists, existsErr := s.Repo.HasAutomaticMoment(ctx, sceneID, src.BeatID, momentIndex)
			if existsErr != nil {
				return created, existsErr
			}
			if exists {
				continue
			}
		}
		cfg, cfgErr := s.activePerchanceConfig(ctx)
		if cfgErr != nil {
			return created, cfgErr
		}
		gid, idErr := id.New()
		if idErr != nil {
			return created, idErr
		}
		now := s.now()
		var beatPtr *id.ID
		if !src.BeatID.IsZero() {
			beat := src.BeatID
			beatPtr = &beat
		}
		g, newErr := domain.New(gid, storyID, sceneID, cfg.ID, beatPtr, cfg.Image, moment.Positive, moment.Negative, domain.Automatic, now)
		if newErr != nil {
			return created, newErr
		}
		g.MomentIndex = momentIndex
		g.AnchorParagraph = moment.AnchorParagraph
		g.PromptSetRevisionID = moment.PromptSetRevisionID
		if createErr := s.Repo.Create(ctx, g); createErr != nil {
			if errors.Is(createErr, imagegenerationrepo.ErrAnchorOccupied) {
				continue
			}
			return created, createErr
		}
		created = append(created, gid)
		if s.Logger != nil {
			s.Logger.Info("illustration moment scheduled", "generation_id", gid, "story_id", storyID, "scene_id", sceneID, "source_beat_id", src.BeatID, "moment_index", momentIndex, "anchor_paragraph", moment.AnchorParagraph)
		}
	}
	if len(created) == 0 {
		return nil, ErrIllustrationExists
	}
	return created, nil
}

type singleMomentBuilder struct{ Prompt ImagePrompt }

func (b singleMomentBuilder) BuildMoments(context.Context, imagegenerationrepo.SceneSource, bool) ([]ImagePrompt, error) {
	return []ImagePrompt{b.Prompt}, nil
}

func (s Service) Schedule(ctx context.Context, storyID, sceneID id.ID) (id.ID, error) {
	ids, err := s.scheduleAutomaticMoments(ctx, storyID, sceneID, "", true)
	if err != nil {
		if s.Logger != nil && !errors.Is(err, ErrIllustrationExists) {
			s.Logger.Warn("image generation schedule failed", "story_id", storyID, "scene_id", sceneID, "error", err)
		}
		return "", err
	}
	return ids[0], nil
}

func (s Service) ScheduleBeat(ctx context.Context, storyID, sceneID id.ID) (id.ID, error) {
	ids, err := s.scheduleAutomaticMoments(ctx, storyID, sceneID, "", false)
	if err != nil {
		if errors.Is(err, ErrIllustrationNotNeeded) || errors.Is(err, ErrIllustrationExists) {
			return "", err
		}
		if s.Logger != nil {
			s.Logger.Warn("beat illustration schedule failed", "story_id", storyID, "scene_id", sceneID, "error", err)
		}
		return "", err
	}
	return ids[0], nil
}

func (s Service) SchedulePromptJob(ctx context.Context, job imagegenerationrepo.PromptJob) error {
	_, err := s.scheduleAutomaticMoments(ctx, job.StoryID, job.SceneID, job.SourceBeatID, job.ForceIllustration)
	if errors.Is(err, ErrIllustrationNotNeeded) || errors.Is(err, ErrIllustrationExists) {
		return nil
	}
	return err
}
