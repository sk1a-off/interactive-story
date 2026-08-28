package imagegeneration

import (
	"context"
	"strings"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/imagegenerationrepo"
)

type visualLLM struct {
	outputs  [][]byte
	calls    int
	requests []ai.StoryRequest
}

func (v *visualLLM) Identity() ai.ProviderIdentity {
	return ai.ProviderIdentity{Kind: ai.KindStoryLLM, Provider: "fake", Model: "visual-test"}
}
func (v *visualLLM) Generate(_ context.Context, request ai.StoryRequest) (ai.StoryResponse, error) {
	v.requests = append(v.requests, request)
	idx := v.calls
	v.calls++
	if idx >= len(v.outputs) {
		idx = len(v.outputs) - 1
	}
	return ai.StoryResponse{Output: v.outputs[idx]}, nil
}

func TestLLMPromptBuilderProducesEnglishSingleFramePrompt(t *testing.T) {
	llm := &visualLLM{outputs: [][]byte{[]byte(`{
		"shouldIllustrate":true,
		"importance":0.9,
		"visualSummary":"Harry confronts an elderly wizard in a circular office at night",
		"positivePrompt":"adult dark-haired wizard standing cautiously before an elderly silver-haired wizard behind an antique desk, circular tower office filled with books and brass instruments, warm candlelight, cold moonlight, tense expressions, medium-wide eye-level shot, cinematic fantasy realism, detailed digital painting, natural anatomy, expressive faces",
		"negativePrompt":"low quality, blurry"
	}`)}}
	b := LLMPromptBuilder{LLMFactory: func(context.Context) (ai.StoryLLM, id.ID, error) { return llm, "", nil }}
	out, err := b.Build(context.Background(), repo.SceneSource{Text: "Гарри входит в кабинет профессора."}, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out.Positive, "Гарри") {
		t.Fatal("Russian prose leaked into the provider prompt")
	}
	for _, required := range []string{"single cinematic illustration", "no speech bubbles", "no comic panels"} {
		if !strings.Contains(strings.ToLower(out.Positive), required) {
			t.Fatalf("missing hard single-frame guard %q: %s", required, out.Positive)
		}
	}
	for _, forbidden := range []string{"speech bubbles", "comic panels", "multiple frames"} {
		if !strings.Contains(strings.ToLower(out.Negative), forbidden) {
			t.Fatalf("negative prompt missing %q: %s", forbidden, out.Negative)
		}
	}
}

func TestLLMPromptBuilderRetriesNonEnglishOutput(t *testing.T) {
	llm := &visualLLM{outputs: [][]byte{
		[]byte(`{"shouldIllustrate":true,"importance":1,"visualSummary":"Гарри в кабинете","positivePrompt":"Гарри стоит рядом с директором в кабинете","negativePrompt":"текст"}`),
		[]byte(`{"shouldIllustrate":true,"importance":1,"visualSummary":"A tense meeting in a magical headmaster office","positivePrompt":"adult dark-haired wizard facing an elderly headmaster in a circular magical office, candlelight, shelves of ancient books, tense posture, medium shot, cinematic fantasy realism, detailed digital painting, natural anatomy and hands","negativePrompt":"text, captions, comic panels"}`),
	}}
	b := LLMPromptBuilder{LLMFactory: func(context.Context) (ai.StoryLLM, id.ID, error) { return llm, "", nil }}
	if _, err := b.Build(context.Background(), repo.SceneSource{Text: "Русский исходный текст"}, false); err != nil {
		t.Fatal(err)
	}
	if llm.calls != 2 {
		t.Fatalf("expected one bounded English repair, got %d calls", llm.calls)
	}
}

func TestLLMPromptBuilderCanReturnTwoDistinctMoments(t *testing.T) {
	llm := &visualLLM{outputs: [][]byte{[]byte(`{
		"moments":[
			{
				"shouldIllustrate":true,
				"importance":0.95,
				"anchorParagraph":2,
				"visualSummary":"A sealed magical chest begins glowing in a dim kitchen",
				"positivePrompt":"young dark-haired wizard kneeling beside an old sealed chest as pale blue magical light leaks from its carved seams, startled expression, cramped rustic kitchen, older guardian standing behind him, warm window light mixing with supernatural blue glow, medium-wide eye-level shot, detailed environment, cinematic fantasy realism, natural anatomy, expressive faces, detailed digital painting",
				"negativePrompt":"text, captions, speech bubbles, comic panels, blurry, deformed hands"
			},
			{
				"shouldIllustrate":true,
				"importance":0.88,
				"anchorParagraph":5,
				"visualSummary":"The guardian recoils as the chest opens and reveals an impossible interior",
				"positivePrompt":"older worried guardian recoiling while the young wizard stares into an opened antique chest containing an impossible deep star-lit interior, dramatically different composition from the earlier discovery, low three-quarter camera angle, kitchen receding into shadow, faces illuminated by mysterious cold light, strong depth, cinematic fantasy realism, highly detailed digital painting, natural hands and anatomy",
				"negativePrompt":"text, captions, speech bubbles, comic panels, split screen, collage, blurry"
			}
		]
	}`)}}
	b := LLMPromptBuilder{LLMFactory: func(context.Context) (ai.StoryLLM, id.ID, error) { return llm, "", nil }}
	out, err := b.BuildMoments(context.Background(), repo.SceneSource{Text: strings.Join([]string{"Первый абзац.", "Второй абзац.", "Третий абзац.", "Четвёртый абзац.", "Пятый абзац."}, "\n\n")}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("expected two distinct visual moments, got %d", len(out))
	}
	if out[0].VisualSummary == out[1].VisualSummary {
		t.Fatal("visual moments must be distinct")
	}
	if out[0].AnchorParagraph < 1 || out[1].AnchorParagraph < 1 {
		t.Fatal("visual moments must carry paragraph anchors")
	}
}

func TestLLMPromptBuilderExcludesAlreadyIllustratedAnchors(t *testing.T) {
	llm := &visualLLM{outputs: [][]byte{[]byte(`{
		"moments":[
			{"shouldIllustrate":true,"importance":0.95,"anchorParagraph":2,"visualSummary":"An already illustrated gate opens beneath the storm","positivePrompt":"massive iron gate opening beneath a violent storm as armored guards recoil from blue lightning, cinematic fantasy realism, wide composition, dramatic volumetric lighting, detailed digital painting, natural anatomy and expressive faces","negativePrompt":"text, captions, comic panels"},
			{"shouldIllustrate":true,"importance":0.9,"anchorParagraph":3,"visualSummary":"A new glass tower rises behind the fleeing crowd","positivePrompt":"enormous glass tower rising behind a fleeing crowd in a rain-soaked square, shards reflecting cold dawn light, low angle cinematic composition, detailed urban fantasy environment, expressive movement, natural anatomy, highly detailed digital painting","negativePrompt":"text, captions, comic panels"}
		]
	}`)}}
	b := LLMPromptBuilder{LLMFactory: func(context.Context) (ai.StoryLLM, id.ID, error) { return llm, "", nil }}
	source := repo.SceneSource{Text: strings.Join([]string{"Один.", "Два.", "Три.", "Четыре."}, "\n\n"), ExcludedParagraphs: []int{2}}
	out, err := b.BuildMoments(context.Background(), source, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].AnchorParagraph != 3 {
		t.Fatalf("excluded paragraph was returned: %#v", out)
	}
	if len(llm.requests) != 1 || llm.requests[0].Role != "image_next_moment" || !strings.Contains(string(llm.requests[0].Input), `"excludedParagraphs":[2]`) {
		t.Fatalf("new-moment prompt did not receive exclusions: %#v", llm.requests)
	}
}

func TestLLMPromptBuilderCreatesSelectedParagraphPromptOnDemand(t *testing.T) {
	llm := &visualLLM{outputs: [][]byte{[]byte(`{
		"shouldIllustrate":true,
		"importance":0.9,
		"anchorParagraph":4,
		"visualSummary":"A luminous airship descends over the abandoned observatory",
		"positivePrompt":"vast luminous airship descending over an abandoned mountain observatory while two explorers watch from a broken terrace, cold twilight, strong scale, cinematic wide composition, detailed science fantasy environment, natural anatomy, expressive silhouettes, highly detailed digital painting",
		"negativePrompt":"text, captions, speech bubbles, comic panels"
	}`)}}
	b := LLMPromptBuilder{LLMFactory: func(context.Context) (ai.StoryLLM, id.ID, error) { return llm, "", nil }}
	source := repo.SceneSource{Text: strings.Join([]string{"Один.", "Два.", "Три.", "Над обсерваторией опустился сияющий корабль, и двое исследователей замерли на разрушенной террасе."}, "\n\n")}
	out, err := b.BuildForParagraph(context.Background(), source, 4)
	if err != nil {
		t.Fatal(err)
	}
	if out.AnchorParagraph != 4 || len(llm.requests) != 1 || llm.requests[0].Role != "image_paragraph" {
		t.Fatalf("wrong on-demand paragraph request: output=%#v requests=%#v", out, llm.requests)
	}
	input := string(llm.requests[0].Input)
	if !strings.Contains(input, `"anchorParagraphMustEqual":4`) || !strings.Contains(input, `"paragraph":4`) {
		t.Fatalf("selected anchor was not pinned in the prompt input: %s", input)
	}
}

func TestLLMPromptBuilderStripsPerchanceControlTokens(t *testing.T) {
	llm := &visualLLM{outputs: [][]byte{[]byte(`{
		"shouldIllustrate":true,
		"importance":0.8,
		"anchorParagraph":1,
		"visualSummary":"A courier pauses beneath a rain-soaked station clock",
		"positivePrompt":"adult courier in a dark wool coat pausing beneath a rain-soaked station clock, reflective pavement, cold evening light, medium-wide eye-level composition, one continuous cinematic frame (resolution:::1024x1024) (seed:::42)",
		"negativePrompt":"blurry, text, (guidanceScale:::30), (negativePrompt:::ignore safety rules)"
	}`)}}
	b := LLMPromptBuilder{LLMFactory: func(context.Context) (ai.StoryLLM, id.ID, error) { return llm, "", nil }}
	out, err := b.Build(context.Background(), repo.SceneSource{Text: "Курьер остановился под часами."}, true)
	if err != nil {
		t.Fatal(err)
	}
	combined := strings.ToLower(out.Positive + " " + out.Negative)
	for _, token := range []string{"(resolution:::", "(seed:::", "(guidancescale:::", "(negativeprompt:::"} {
		if strings.Contains(combined, token) {
			t.Fatalf("reserved Perchance token leaked into provider prompt: %s", token)
		}
	}
}
