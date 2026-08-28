package imagegeneration

import (
	"errors"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

type Status string

const (
	Pending Status = "pending"
	Running Status = "running"
	Done    Status = "done"
	Failed  Status = "failed"
)

type Trigger string

const (
	Automatic Trigger = "automatic"
	Manual    Trigger = "manual"
)
const (
	DefaultWidth               = 768
	DefaultHeight              = 768
	DefaultCount               = 2
	DefaultStyle               = "digital-painting"
	DefaultStylePrompt         = "cinematic digital illustration, detailed painterly rendering, controlled bold shading, physically coherent global illumination, expressive faces, readable body language, natural anatomy, realistic hands, integrated atmospheric background, strong composition, clear depth, sharp focal subject, rich but balanced color"
	DefaultStyleNegativePrompt = "text, letters, words, captions, subtitles, speech bubbles, dialogue balloons, comic book, comic panels, manga panels, storyboard, split screen, collage, multiple frames, contact sheet, watermark, signature, logo, UI, duplicated characters, duplicate face, extra limbs, extra arms, extra legs, extra fingers, fused fingers, malformed hands, deformed anatomy, cropped head, blurry face, flat lighting, cluttered composition, low detail, low quality, jpeg artifacts"
	DefaultMaxAttempts         = 3
)

var (
	ErrInvalid           = errors.New("invalid image generation")
	ErrInvalidTransition = errors.New("invalid image generation transition")
	ErrLeaseMismatch     = errors.New("image generation lease mismatch")
)

type Generation struct {
	ID                  id.ID               `json:"id"`
	StoryID             id.ID               `json:"storyId"`
	SceneID             id.ID               `json:"sceneId"`
	SourceBeatID        *id.ID              `json:"sourceBeatId,omitempty"`
	MomentIndex         int                 `json:"momentIndex"`
	AnchorParagraph     int                 `json:"anchorParagraph,omitempty"`
	ConfigRevisionID    id.ID               `json:"configRevisionId"`
	PromptSetRevisionID id.ID               `json:"promptSetRevisionId,omitempty"`
	Provider            ai.ProviderIdentity `json:"provider"`
	Prompt              string              `json:"prompt"`
	NegativePrompt      string              `json:"negativePrompt"`
	Style               string              `json:"style"`
	StylePrompt         string              `json:"stylePrompt"`
	StyleNegativePrompt string              `json:"styleNegativePrompt"`
	Width               int                 `json:"width"`
	Height              int                 `json:"height"`
	ImageCount          int                 `json:"imageCount"`
	Trigger             Trigger             `json:"trigger"`
	Status              Status              `json:"status"`
	Attempts            int                 `json:"attempts"`
	MaxAttempts         int                 `json:"maxAttempts"`
	AvailableAt         time.Time           `json:"availableAt"`
	LeaseOwner          string              `json:"-"`
	LeaseUntil          *time.Time          `json:"-"`
	ErrorCode           string              `json:"errorCode,omitempty"`
	CreatedAt           time.Time           `json:"createdAt"`
	StartedAt           *time.Time          `json:"startedAt,omitempty"`
	FinishedAt          *time.Time          `json:"finishedAt,omitempty"`
}
type Image struct {
	ID           id.ID     `json:"id"`
	StoryID      id.ID     `json:"storyId"`
	SceneID      id.ID     `json:"sceneId"`
	GenerationID id.ID     `json:"generationId"`
	Variant      int       `json:"variant"`
	URL          string    `json:"url"`
	ContentType  string    `json:"contentType"`
	ByteSize     int64     `json:"byteSize"`
	CreatedAt    time.Time `json:"createdAt"`
}
type View struct {
	Generation      Generation `json:"generation"`
	Images          []Image    `json:"images"`
	SelectedImageID *id.ID     `json:"selectedImageId,omitempty"`
}

func New(gid, storyID, sceneID, configID id.ID, beatID *id.ID, p ai.ProviderIdentity, prompt, negative string, trigger Trigger, now time.Time) (Generation, error) {
	if gid.IsZero() || storyID.IsZero() || sceneID.IsZero() || configID.IsZero() || p.Kind != ai.KindImage || p.Provider == "" || p.Model == "" || prompt == "" {
		return Generation{}, ErrInvalid
	}
	if trigger != Automatic && trigger != Manual {
		return Generation{}, ErrInvalid
	}
	return Generation{ID: gid, StoryID: storyID, SceneID: sceneID, SourceBeatID: beatID, MomentIndex: 1, ConfigRevisionID: configID, Provider: p, Prompt: prompt, NegativePrompt: negative, Style: DefaultStyle, StylePrompt: DefaultStylePrompt, StyleNegativePrompt: DefaultStyleNegativePrompt, Width: DefaultWidth, Height: DefaultHeight, ImageCount: DefaultCount, Trigger: trigger, Status: Pending, MaxAttempts: DefaultMaxAttempts, AvailableAt: now, CreatedAt: now}, nil
}
func (g Generation) CanClaim(now time.Time) bool {
	if g.Status == Running && g.LeaseUntil != nil && g.LeaseUntil.After(now) {
		return false
	}
	return (g.Status == Pending || g.Status == Running) && !now.Before(g.AvailableAt) && g.Attempts < g.MaxAttempts
}

func RetryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return 5 * time.Second
	}
	return 20 * time.Second
}
func AttemptsExhausted(attempts, max int) bool { return max > 0 && attempts >= max }
