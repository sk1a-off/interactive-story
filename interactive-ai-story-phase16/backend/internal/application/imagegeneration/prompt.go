package imagegeneration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/imagegenerationrepo"
)

const HardNegativePrompt = "text, letters, words, captions, subtitles, speech bubbles, dialogue balloons, comic book, comic panels, manga panels, storyboard, split screen, collage, multiple frames, contact sheet, watermark, signature, logo, UI, duplicated characters, duplicate face, extra limbs, extra arms, extra legs, extra fingers, malformed hands, deformed anatomy, cropped head, blurry face, low detail"

var ErrInvalidVisualPrompt = errors.New("visual prompt director returned an invalid image prompt")

var perchanceControlTokenRE = regexp.MustCompile(`(?i)\((?:negativeprompt|resolution|seed|guidancescale):::[^)]*\)`)

type ImagePrompt struct {
	PromptSetRevisionID id.ID
	Positive            string
	Negative            string
	VisualSummary       string
	ShouldIllustrate    bool
	Importance          float64
	AnchorParagraph     int
}

type SceneImagePromptBuilder interface {
	Build(context.Context, imagegenerationrepo.SceneSource, bool) (ImagePrompt, error)
}

type SceneImageMomentPromptBuilder interface {
	BuildMoments(context.Context, imagegenerationrepo.SceneSource, bool) ([]ImagePrompt, error)
}

type SceneParagraphPromptBuilder interface {
	BuildForParagraph(context.Context, imagegenerationrepo.SceneSource, int) (ImagePrompt, error)
}

type StoryLLMFactory func(context.Context) (ai.StoryLLM, id.ID, error)

type LLMPromptBuilder struct {
	LLMFactory StoryLLMFactory
}

type visualPromptResponse struct {
	ShouldIllustrate bool    `json:"shouldIllustrate"`
	Importance       float64 `json:"importance"`
	VisualSummary    string  `json:"visualSummary"`
	PositivePrompt   string  `json:"positivePrompt"`
	NegativePrompt   string  `json:"negativePrompt"`
	AnchorParagraph  int     `json:"anchorParagraph"`
}

type visualMomentsResponse struct {
	Moments []visualPromptResponse `json:"moments"`
}

func numberedParagraphs(text string) []map[string]any {
	parts := narrative.DisplayParagraphs(text)
	out := make([]map[string]any, 0, len(parts))
	for i, part := range parts {
		out = append(out, map[string]any{
			"paragraph": i + 1,
			"text":      part,
		})
	}
	return out
}

func clampParagraphAnchor(anchor, paragraphCount int) int {
	if paragraphCount <= 0 {
		return 1
	}
	if anchor < 1 {
		return max(1, (paragraphCount+1)/2)
	}
	if anchor > paragraphCount {
		return paragraphCount
	}
	return anchor
}

func (b LLMPromptBuilder) Build(ctx context.Context, s imagegenerationrepo.SceneSource, force bool) (ImagePrompt, error) {
	if b.LLMFactory == nil {
		return ImagePrompt{}, errors.New("visual prompt LLM unavailable")
	}
	llm, promptSetID, err := b.LLMFactory(ctx)
	if err != nil {
		return ImagePrompt{}, err
	}
	input := map[string]any{
		"forceIllustration": force,
		"scene": map[string]any{
			"mood":           s.Mood,
			"goal":           s.Goal,
			"latestBeatText": s.Text,
			"paragraphs":     numberedParagraphs(s.Text),
		},
		"storyBible":  jsonValue(s.StoryBible),
		"player":      jsonValue(s.Player),
		"world":       jsonValue(s.World),
		"initialCast": jsonValue(s.InitialCast),
		"visualBible": jsonValue(s.VisualBible),
		"requirements": map[string]any{
			"outputLanguage":        "English only",
			"imageModelPromptStyle": "Stable Diffusion style visual prompt",
			"singleFrame":           true,
			"noTextInImage":         true,
			"noComicPanels":         true,
		},
	}
	var out visualPromptResponse
	if err := generateVisualPrompt(ctx, llm, input, &out); err != nil {
		return ImagePrompt{}, err
	}
	if !validVisualPrompt(out) {
		repair := map[string]any{
			"forceIllustration":     force,
			"scene":                 input["scene"],
			"storyBible":            input["storyBible"],
			"player":                input["player"],
			"world":                 input["world"],
			"initialCast":           input["initialCast"],
			"visualBible":           input["visualBible"],
			"previousInvalidOutput": out,
			"instruction":           "Rewrite positivePrompt, negativePrompt and visualSummary in English only. Depict exactly one frozen cinematic frame. Do not include comic panels, speech bubbles, captions or multiple chronological moments.",
		}
		if err := generateVisualPrompt(ctx, llm, repair, &out); err != nil {
			return ImagePrompt{}, err
		}
	}
	if !validVisualPrompt(out) {
		return ImagePrompt{}, ErrInvalidVisualPrompt
	}
	return ImagePrompt{
		PromptSetRevisionID: promptSetID,
		Positive:            normalizePositive(out.PositivePrompt),
		Negative:            mergeNegative(out.NegativePrompt, HardNegativePrompt),
		VisualSummary:       strings.TrimSpace(out.VisualSummary),
		ShouldIllustrate:    out.ShouldIllustrate,
		Importance:          clamp01(out.Importance),
		AnchorParagraph:     clampParagraphAnchor(out.AnchorParagraph, len(narrative.DisplayParagraphs(s.Text))),
	}, nil
}

func (b LLMPromptBuilder) BuildMoments(ctx context.Context, s imagegenerationrepo.SceneSource, forceAtLeastOne bool) ([]ImagePrompt, error) {
	if b.LLMFactory == nil {
		return nil, errors.New("visual prompt LLM unavailable")
	}
	llm, promptSetID, err := b.LLMFactory(ctx)
	if err != nil {
		return nil, err
	}
	input := map[string]any{
		"forceAtLeastOne": forceAtLeastOne,
		"maxMoments":      2,
		"scene": map[string]any{
			"mood":           s.Mood,
			"goal":           s.Goal,
			"latestBeatText": s.Text,
			"paragraphs":     numberedParagraphs(s.Text),
		},
		"excludedParagraphs": s.ExcludedParagraphs,
		"storyBible":         jsonValue(s.StoryBible),
		"player":             jsonValue(s.Player),
		"world":              jsonValue(s.World),
		"initialCast":        jsonValue(s.InitialCast),
		"visualBible":        jsonValue(s.VisualBible),
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	resp, err := llm.Generate(ctx, ai.StoryRequest{Role: "image_next_moment", PromptVersion: "v1", Input: raw})
	if err != nil {
		return nil, err
	}
	var decoded visualMomentsResponse
	if err := json.Unmarshal(resp.Output, &decoded); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidVisualPrompt, err)
	}
	if len(decoded.Moments) > 2 {
		decoded.Moments = decoded.Moments[:2]
	}
	out := make([]ImagePrompt, 0, len(decoded.Moments))
	seenSummary := map[string]struct{}{}
	seenAnchor := map[int]struct{}{}
	for _, anchor := range s.ExcludedParagraphs {
		seenAnchor[anchor] = struct{}{}
	}
	for _, moment := range decoded.Moments {
		if !moment.ShouldIllustrate || !validVisualPrompt(moment) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(moment.VisualSummary))
		if _, exists := seenSummary[key]; exists {
			continue
		}
		anchor := clampParagraphAnchor(moment.AnchorParagraph, len(narrative.DisplayParagraphs(s.Text)))
		if _, exists := seenAnchor[anchor]; exists {
			continue
		}
		seenSummary[key] = struct{}{}
		seenAnchor[anchor] = struct{}{}
		out = append(out, ImagePrompt{
			PromptSetRevisionID: promptSetID,
			Positive:            normalizePositive(moment.PositivePrompt),
			Negative:            mergeNegative(moment.NegativePrompt, HardNegativePrompt),
			VisualSummary:       strings.TrimSpace(moment.VisualSummary),
			ShouldIllustrate:    true,
			Importance:          clamp01(moment.Importance),
			AnchorParagraph:     anchor,
		})
	}
	if forceAtLeastOne && len(out) == 0 && len(s.ExcludedParagraphs) == 0 {
		one, err := b.Build(ctx, s, true)
		if err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, nil
}

func (b LLMPromptBuilder) BuildForParagraph(ctx context.Context, s imagegenerationrepo.SceneSource, paragraph int) (ImagePrompt, error) {
	paragraphs := narrative.DisplayParagraphs(s.Text)
	if paragraph < 1 || paragraph > len(paragraphs) {
		return ImagePrompt{}, ErrInvalidVisualPrompt
	}
	if b.LLMFactory == nil {
		return ImagePrompt{}, errors.New("visual prompt LLM unavailable")
	}
	llm, promptSetID, err := b.LLMFactory(ctx)
	if err != nil {
		return ImagePrompt{}, err
	}
	input := map[string]any{
		"forceIllustration":        true,
		"targetParagraph":          map[string]any{"paragraph": paragraph, "text": paragraphs[paragraph-1]},
		"anchorParagraphMustEqual": paragraph,
		"scene":                    map[string]any{"mood": s.Mood, "goal": s.Goal, "latestBeatText": s.Text, "paragraphs": numberedParagraphs(s.Text)},
		"storyBible":               jsonValue(s.StoryBible), "player": jsonValue(s.Player), "world": jsonValue(s.World), "initialCast": jsonValue(s.InitialCast), "visualBible": jsonValue(s.VisualBible),
		"instruction": "Create the prompt on demand for exactly the selected paragraph. Depict its best frozen visual instant. Do not move to another paragraph and do not turn spoken words into visible text.",
	}
	var out visualPromptResponse
	if err = generateParagraphVisualPrompt(ctx, llm, input, &out); err != nil {
		return ImagePrompt{}, err
	}
	if !validVisualPrompt(out) {
		input["previousInvalidOutput"] = out
		input["instruction"] = "Repair the response in English only for exactly the selected paragraph. Return one frozen visual frame, never dialogue text, captions or panels."
		if err = generateParagraphVisualPrompt(ctx, llm, input, &out); err != nil {
			return ImagePrompt{}, err
		}
	}
	if !validVisualPrompt(out) {
		return ImagePrompt{}, ErrInvalidVisualPrompt
	}
	return ImagePrompt{PromptSetRevisionID: promptSetID, Positive: normalizePositive(out.PositivePrompt), Negative: mergeNegative(out.NegativePrompt, HardNegativePrompt), VisualSummary: strings.TrimSpace(out.VisualSummary), ShouldIllustrate: true, Importance: clamp01(out.Importance), AnchorParagraph: paragraph}, nil
}

func generateParagraphVisualPrompt(ctx context.Context, llm ai.StoryLLM, input any, out *visualPromptResponse) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	resp, err := llm.Generate(ctx, ai.StoryRequest{Role: "image_paragraph", PromptVersion: "v1", Input: raw})
	if err != nil {
		return err
	}
	if err := json.Unmarshal(resp.Output, out); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidVisualPrompt, err)
	}
	return nil
}

func generateVisualPrompt(ctx context.Context, llm ai.StoryLLM, input any, out *visualPromptResponse) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	resp, err := llm.Generate(ctx, ai.StoryRequest{Role: "image_prompt", PromptVersion: "v1", Input: raw})
	if err != nil {
		return err
	}
	if err := json.Unmarshal(resp.Output, out); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidVisualPrompt, err)
	}
	return nil
}

func validVisualPrompt(v visualPromptResponse) bool {
	return strings.TrimSpace(v.VisualSummary) != "" &&
		strings.TrimSpace(v.PositivePrompt) != "" &&
		englishEnough(v.VisualSummary, 12) &&
		englishEnough(v.PositivePrompt, 20) &&
		(strings.TrimSpace(v.NegativePrompt) == "" || englishEnough(v.NegativePrompt, 3))
}

func englishOnlyEnough(s string) bool { return englishEnough(s, 20) }

func englishEnough(s string, minLatin int) bool {
	latin, cyrillic := 0, 0
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		switch {
		case unicode.In(r, unicode.Cyrillic):
			cyrillic++
		case unicode.In(r, unicode.Latin):
			latin++
		}
	}
	return cyrillic == 0 && latin >= minLatin
}

func normalizePositive(s string) string {
	s = stripPerchanceControlTokens(strings.TrimSpace(strings.Trim(s, "`")))
	guard := "single cinematic illustration, one continuous scene, one frame, no text, no captions, no speech bubbles, no comic panels"
	return guard + ", " + s
}

func mergeNegative(parts ...string) string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 32)
	for _, part := range parts {
		part = stripPerchanceControlTokens(part)
		for _, token := range strings.Split(part, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			key := strings.ToLower(token)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, token)
		}
	}
	return strings.Join(out, ", ")
}

func stripPerchanceControlTokens(s string) string {
	return strings.TrimSpace(perchanceControlTokenRE.ReplaceAllString(s, ""))
}

func jsonValue(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}
	}
	var v any
	if json.Unmarshal([]byte(raw), &v) == nil {
		return v
	}
	return raw
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// DeterministicPromptBuilder remains useful for isolated tests and emergency
// degraded mode, but production wiring uses LLMPromptBuilder so Russian story
// prose is never passed directly to Perchance.
type DeterministicPromptBuilder struct{}

func (d DeterministicPromptBuilder) BuildMoments(ctx context.Context, s imagegenerationrepo.SceneSource, forceAtLeastOne bool) ([]ImagePrompt, error) {
	one, err := d.Build(ctx, s, forceAtLeastOne)
	if err != nil {
		return nil, err
	}
	return []ImagePrompt{one}, nil
}

func (DeterministicPromptBuilder) Build(_ context.Context, s imagegenerationrepo.SceneSource, _ bool) (ImagePrompt, error) {
	text := strings.TrimSpace(s.Text)
	if utf8.RuneCountInString(text) > 240 {
		text = string([]rune(text)[:240])
	}
	positive := "single cinematic fantasy illustration, one continuous scene, detailed environment, expressive characters, natural body language, cinematic lighting, painterly realism, high detail"
	if englishOnlyEnough(text) {
		positive += ", " + text
	}
	paragraphs := narrative.DisplayParagraphs(s.Text)
	return ImagePrompt{Positive: normalizePositive(positive), Negative: HardNegativePrompt, VisualSummary: "cinematic scene illustration", ShouldIllustrate: true, Importance: 1, AnchorParagraph: clampParagraphAnchor(0, len(paragraphs))}, nil
}
