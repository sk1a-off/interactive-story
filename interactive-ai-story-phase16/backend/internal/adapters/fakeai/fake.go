package fakeai

import (
	"context"
	"fmt"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

type StoryLLM struct {
	ID     aiport.ProviderIdentity
	Output []byte
	Err    error
	Calls  int
}

func NewStoryLLM(output []byte) *StoryLLM {
	return &StoryLLM{ID: aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "fake", Model: "deterministic-story-v1", Profile: "test"}, Output: append([]byte(nil), output...)}
}
func (f *StoryLLM) Identity() aiport.ProviderIdentity { return f.ID }
func (f *StoryLLM) Generate(ctx context.Context, _ aiport.StoryRequest) (aiport.StoryResponse, error) {
	select {
	case <-ctx.Done():
		return aiport.StoryResponse{}, ctx.Err()
	default:
	}
	f.Calls++
	if f.Err != nil {
		return aiport.StoryResponse{}, f.Err
	}
	return aiport.StoryResponse{Output: append([]byte(nil), f.Output...)}, nil
}

type EmbeddingProvider struct {
	ID    aiport.ProviderIdentity
	Dims  int
	Calls int
}

func NewEmbeddingProvider(d int) *EmbeddingProvider {
	return &EmbeddingProvider{ID: aiport.ProviderIdentity{Kind: aiport.KindEmbedding, Provider: "fake", Model: "deterministic-embedding-v1", Profile: "test"}, Dims: d}
}
func (f *EmbeddingProvider) Identity() aiport.ProviderIdentity { return f.ID }
func (f *EmbeddingProvider) Dimensions() int                   { return f.Dims }
func (f *EmbeddingProvider) embed(ctx context.Context, req aiport.EmbeddingRequest) (aiport.EmbeddingResponse, error) {
	select {
	case <-ctx.Done():
		return aiport.EmbeddingResponse{}, ctx.Err()
	default:
	}
	f.Calls++
	out := make([][]float32, len(req.Texts))
	for i, t := range req.Texts {
		out[i] = make([]float32, f.Dims)
		for j := range out[i] {
			out[i][j] = float32((len(t)+i+j)%17) / 17
		}
	}
	return aiport.EmbeddingResponse{Vectors: out}, nil
}

func (f *EmbeddingProvider) EmbedDocuments(ctx context.Context, req aiport.EmbeddingRequest) (aiport.EmbeddingResponse, error) {
	return f.embed(ctx, req)
}
func (f *EmbeddingProvider) EmbedQueries(ctx context.Context, req aiport.EmbeddingRequest) (aiport.EmbeddingResponse, error) {
	return f.embed(ctx, req)
}
func (f *EmbeddingProvider) Capabilities(context.Context) (aiport.EmbeddingCapabilities, error) {
	return aiport.EmbeddingCapabilities{Dimensions: f.Dims, Device: "fake", Model: f.ID.Model}, nil
}

type ImageProvider struct {
	ID    aiport.ProviderIdentity
	Calls int
	Err   error
}

func NewImageProvider() *ImageProvider {
	return &ImageProvider{ID: aiport.ProviderIdentity{Kind: aiport.KindImage, Provider: "fake", Model: "deterministic-image-v1", Profile: "test"}}
}
func (f *ImageProvider) Identity() aiport.ProviderIdentity { return f.ID }
func (f *ImageProvider) Generate(ctx context.Context, req aiport.ImageRequest) (aiport.ImageResponse, error) {
	select {
	case <-ctx.Done():
		return aiport.ImageResponse{}, ctx.Err()
	default:
	}
	f.Calls++
	if f.Err != nil {
		return aiport.ImageResponse{}, f.Err
	}
	return aiport.ImageResponse{ArtifactRef: fmt.Sprintf("fake://image/%dx%d/%d", req.Width, req.Height, len(req.Prompt))}, nil
}

type ScriptedStoryLLM struct {
	ID        ProviderIdentityAlias
	Responses map[string][]byte
	ErrByRole map[string]error
	Calls     []string
	Requests  []aiport.StoryRequest
}
type ProviderIdentityAlias = aiport.ProviderIdentity

func NewScriptedStoryLLM(responses map[string][]byte) *ScriptedStoryLLM {
	return &ScriptedStoryLLM{ID: aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "fake", Model: "scripted-story-v1", Profile: "vertical-slice"}, Responses: responses, ErrByRole: map[string]error{}}
}
func (f *ScriptedStoryLLM) Identity() aiport.ProviderIdentity { return f.ID }
func (f *ScriptedStoryLLM) Generate(ctx context.Context, req aiport.StoryRequest) (aiport.StoryResponse, error) {
	select {
	case <-ctx.Done():
		return aiport.StoryResponse{}, ctx.Err()
	default:
	}
	f.Calls = append(f.Calls, req.Role)
	f.Requests = append(f.Requests, req)
	if err := f.ErrByRole[req.Role]; err != nil {
		return aiport.StoryResponse{}, err
	}
	out, ok := f.Responses[req.Role]
	if !ok {
		return aiport.StoryResponse{}, aiport.ErrInvalidOutput
	}
	return aiport.StoryResponse{Output: append([]byte(nil), out...)}, nil
}
