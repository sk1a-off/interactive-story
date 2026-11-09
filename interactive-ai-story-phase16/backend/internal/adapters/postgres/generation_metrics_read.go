package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationmetrics"
)

func (s *GenerationMetrics) Read(ctx context.Context, gid, owner id.ID) (generationmetrics.Report, error) {
	r := generationmetrics.Report{GenerationID: gid, Attempts: []generationmetrics.AttemptTiming{}, Roles: []generationmetrics.RoleTiming{}, Notes: []string{
		"Telemetry is best effort; absent rows do not mean zero elapsed time or zero calls.",
		"writerReadyMs measures completed writer output, not browser rendering. Pipeline time excludes queue wait; parallel role durations overlap.",
		"Token/retry counts are as recorded. Zero usage can mean unreported usage; physical HTTP attempts and polling are not reconstructed.",
	}}
	if owner.IsZero() {
		return r, generationmetrics.ErrNotFound
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `SELECT j.status,j.attempt_count FROM generation_jobs j JOIN stories s ON s.id=j.story_id WHERE j.id=$1 AND s.owner_id=$2 AND s.status<>'deleted'`, gid, owner).Scan(&r.Status, &r.AttemptCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, generationmetrics.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	rows, err := tx.Query(ctx, `SELECT attempt_no,queue_wait_ms,time_to_first_story_text_ms,total_ms,succeeded FROM generation_run_metrics WHERE generation_job_id=$1 ORDER BY attempt_no`, gid)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var a generationmetrics.AttemptTiming
		if err = rows.Scan(&a.Attempt, &a.QueueWaitMS, &a.WriterReadyMS, &a.PipelineTotalMS, &a.Succeeded); err != nil {
			rows.Close()
			return r, err
		}
		r.Attempts = append(r.Attempts, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	rows, err = tx.Query(ctx, `SELECT attempt_no,role,model_name,count(*),COALESCE(sum(latency_ms),0),COALESCE(sum(input_tokens),0),COALESCE(sum(output_tokens),0),COALESCE(sum(repair_count),0),COALESCE(sum(retry_count),0),count(*) FILTER(WHERE error_text<>'')
 FROM generation_attempts WHERE generation_job_id=$1 GROUP BY attempt_no,role,model_name ORDER BY attempt_no,role,model_name`, gid)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var a generationmetrics.RoleTiming
		if err = rows.Scan(&a.Attempt, &a.Role, &a.Model, &a.Calls, &a.RecordedLatencyMS, &a.InputTokens, &a.OutputTokens, &a.Repairs, &a.RecordedRetries, &a.Failures); err != nil {
			rows.Close()
			return r, err
		}
		r.Roles = append(r.Roles, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	return r, tx.Commit(ctx)
}
