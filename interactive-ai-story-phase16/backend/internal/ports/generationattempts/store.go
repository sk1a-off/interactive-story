package generationattempts

import (
	"context"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

// Call is immutable provenance for one physical Story LLM request.
type Call struct {
	GenerationJobID id.ID
	AttemptNo       int
	CallSequence    int
	Role            string
	ContractVersion string
	ContextMode     string
	Provider        aiport.ProviderIdentity
	LatencyMS       int64
	InputTokens     int64
	OutputTokens    int64
	RetryCount      int
	RepairCount     int
	EscalatedFrom   string
	ErrorText       string
}

type Store interface {
	Record(context.Context, Call) error
}
