package generation

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationattempts"
)

type observingLLM struct {
	base        aiport.StoryLLM
	store       generationattempts.Store
	logger      *slog.Logger
	jobID       id.ID
	attemptNo   int
	contextMode string
	mu          sync.Mutex
	sequence    int
}

func (l *observingLLM) Identity() aiport.ProviderIdentity { return l.base.Identity() }

func (l *observingLLM) Generate(ctx context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	l.mu.Lock()
	l.sequence++
	sequence := l.sequence
	l.mu.Unlock()

	started := time.Now()
	response, callErr := l.base.Generate(ctx, request)
	provider := response.Provider
	if provider.Model == "" {
		provider = l.base.Identity()
	}
	errorText := ""
	if callErr != nil {
		errorText = callErr.Error()
	}
	repairCount := 0
	if request.Repair || request.Role == "structured_repair" {
		repairCount = 1
	}
	recordCtx, cancelRecord := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancelRecord()
	recordErr := l.store.Record(recordCtx, generationattempts.Call{
		GenerationJobID: l.jobID, AttemptNo: l.attemptNo, CallSequence: sequence,
		Role: request.Role, ContractVersion: request.PromptVersion, ContextMode: l.contextMode,
		Provider: provider, LatencyMS: time.Since(started).Milliseconds(),
		InputTokens: response.InputTokens, OutputTokens: response.OutputTokens,
		RepairCount: repairCount, EscalatedFrom: response.EscalatedFrom, ErrorText: errorText,
	})
	if recordErr != nil && l.logger != nil {
		l.logger.Warn("generation call telemetry write failed", "generation_id", l.jobID, "role", request.Role, "error", recordErr)
	}
	return response, callErr
}
