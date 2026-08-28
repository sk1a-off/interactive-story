package ai

import (
	"context"
	"errors"
)

var (
	ErrUnavailable   = errors.New("ai provider unavailable")
	ErrInvalidOutput = errors.New("ai provider returned invalid output")
)

type ProviderKind string

const (
	KindStoryLLM  ProviderKind = "story_llm"
	KindEmbedding ProviderKind = "embedding"
	KindImage     ProviderKind = "image"
)

type ProviderIdentity struct {
	Kind     ProviderKind `json:"kind"`
	Provider string       `json:"provider"`
	Model    string       `json:"model"`
	Profile  string       `json:"profile"`
}

type StoryRequest struct {
	Role          string
	PromptVersion string
	Input         []byte
	// MaxTokens optionally narrows the role-level completion budget for a
	// particular request. Zero keeps the configured role default.
	MaxTokens int
}
type StoryResponse struct {
	Output       []byte
	InputTokens  int64
	OutputTokens int64
}
type StoryLLM interface {
	Identity() ProviderIdentity
	Generate(context.Context, StoryRequest) (StoryResponse, error)
}

type EmbeddingRequest struct{ Texts []string }
type EmbeddingResponse struct{ Vectors [][]float32 }
type EmbeddingCapabilities struct {
	Dimensions int
	Device     string
	Model      string
}
type EmbeddingProvider interface {
	Identity() ProviderIdentity
	Dimensions() int
	EmbedDocuments(context.Context, EmbeddingRequest) (EmbeddingResponse, error)
	EmbedQueries(context.Context, EmbeddingRequest) (EmbeddingResponse, error)
	Capabilities(context.Context) (EmbeddingCapabilities, error)
}

type ImageRequest struct {
	Prompt        string
	Width, Height int
}
type ImageResponse struct{ ArtifactRef string }
type ImageProvider interface {
	Identity() ProviderIdentity
	Generate(context.Context, ImageRequest) (ImageResponse, error)
}
