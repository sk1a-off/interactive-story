package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationmetrics"
)

type GenerationMetrics struct{ pool *pgxpool.Pool }

func NewGenerationMetrics(pool *pgxpool.Pool) *GenerationMetrics {
	return &GenerationMetrics{pool: pool}
}

func (s *GenerationMetrics) Record(ctx context.Context, m generationmetrics.Metrics) error {
	metricID, err := id.New()
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO generation_run_metrics(
id,generation_job_id,attempt_no,config_revision_id,prompt_set_revision_id,context_mode,
queue_wait_ms,target_load_ms,context_build_ms,planner_ms,writer_ms,post_writer_ms,
context_shadow_ms,shadow_retrieved_count,shadow_retrieval_error,choices_fallback_used,action_intent_fallback_used,
validation_ms,commit_ms,unattributed_ms,total_ms,time_to_first_story_text_ms,succeeded,error_text)
VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)`,
		metricID, m.GenerationJobID, m.AttemptNo, m.ConfigRevisionID, m.PromptSetRevisionID,
		m.ContextMode, m.QueueWaitMS, m.TargetLoadMS, m.ContextBuildMS, m.PlannerMS,
		m.WriterMS, m.PostWriterMS, m.ContextShadowMS, m.ShadowRetrievedCount, m.ShadowRetrievalError, m.ChoicesFallbackUsed, m.ActionIntentFallbackUsed,
		m.ValidationMS, m.CommitMS, m.UnattributedMS, m.TotalMS, m.TimeToFirstStoryTextMS,
		m.Succeeded, m.ErrorText)
	return err
}
