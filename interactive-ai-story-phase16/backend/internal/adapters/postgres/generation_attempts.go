package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationattempts"
)

type GenerationAttempts struct{ pool *pgxpool.Pool }

func NewGenerationAttempts(pool *pgxpool.Pool) *GenerationAttempts {
	return &GenerationAttempts{pool: pool}
}

func (s *GenerationAttempts) Record(ctx context.Context, call generationattempts.Call) error {
	attemptID, err := id.New()
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO generation_attempts(
id,generation_job_id,attempt_no,call_sequence,role,phase,prompt_version,context_mode,
provider_name,model_name,profile_name,latency_ms,input_tokens,output_tokens,retry_count,
repair_count,escalated_from,error_text)
VALUES($1,$2,$3,$4,$5,'llm',$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		attemptID, call.GenerationJobID, call.AttemptNo, call.CallSequence, call.Role,
		call.ContractVersion, call.ContextMode, call.Provider.Provider, call.Provider.Model,
		call.Provider.Profile, call.LatencyMS, call.InputTokens, call.OutputTokens,
		call.RetryCount, call.RepairCount, call.EscalatedFrom, call.ErrorText)
	return err
}
