package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationattempts"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationmetrics"
)

type metricsReadFixture struct {
	pgx.Tx
	t *testing.T
}

func (f metricsReadFixture) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	if options.AccessMode != pgx.ReadOnly || options.IsoLevel != pgx.RepeatableRead {
		f.t.Fatal("metrics reader lost read-only consistent snapshot")
	}
	return f.Tx.Begin(ctx)
}
func TestGenerationMetricsExposeRecordedCountsWithoutForeignOrErrorData(t *testing.T) {
	ctx, tx, story, tid := memoryOutboxFixture(t)
	var owner, config id.ID
	if err := tx.QueryRow(ctx, `SELECT owner_id FROM stories WHERE id=$1`, story).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO ai_config_revisions(story_llm_provider,story_llm_model,story_llm_profile,embedding_provider,embedding_model,image_provider,image_model,settings)
 VALUES('openai_compatible','test','test','fake','384','manual','manual-v1','{}') RETURNING id`).Scan(&config); err != nil {
		t.Fatal(err)
	}
	job, _ := id.New()
	if _, err := tx.Exec(ctx, `INSERT INTO generation_jobs(id,story_id,timeline_id,expected_head_event_seq,config_revision_id,status,provider_kind,provider_name,model_name,action_hash,attempt_count)
 VALUES($1,$2,$3,2,$4,'completed','story_llm','fake','test',repeat('0',64),2)`, job, story, tid, config); err != nil {
		t.Fatal(err)
	}
	metrics := pg.NewGenerationMetrics(metricsReadFixture{Tx: tx, t: t})
	for _, m := range []generationmetrics.Metrics{
		{GenerationJobID: job, ConfigRevisionID: config, AttemptNo: 1, ContextMode: "test", QueueWaitMS: 5, TotalMS: 100, Succeeded: false, ErrorText: "PRIVATE_ERROR_MUST_NOT_BE_EXPOSED"},
		{GenerationJobID: job, ConfigRevisionID: config, AttemptNo: 2, ContextMode: "test", QueueWaitMS: 10, TimeToFirstStoryTextMS: 1200, TotalMS: 2000, Succeeded: true},
	} {
		if err := metrics.Record(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	// Insert immutable role records within the rolled-back fixture transaction.
	for _, c := range []generationattempts.Call{
		{AttemptNo: 1, CallSequence: 1, Role: "writer", LatencyMS: 100, ErrorText: "PRIVATE_ERROR_MUST_NOT_BE_EXPOSED"},
		{AttemptNo: 2, CallSequence: 1, Role: "writer", LatencyMS: 900, InputTokens: 100, OutputTokens: 200},
		{AttemptNo: 2, CallSequence: 2, Role: "structured_repair", LatencyMS: 200, InputTokens: 20, OutputTokens: 30, RepairCount: 1, RetryCount: 2},
	} {
		cid, _ := id.New()
		if _, err := tx.Exec(ctx, `INSERT INTO generation_attempts(id,generation_job_id,attempt_no,call_sequence,role,phase,prompt_version,provider_name,model_name,profile_name,latency_ms,input_tokens,output_tokens,repair_count,retry_count,error_text)
 VALUES($1,$2,$3,$4,$5,'llm','v1','fake','test','test',$6,$7,$8,$9,$10,$11)`, cid, job, c.AttemptNo, c.CallSequence, c.Role, c.LatencyMS, c.InputTokens, c.OutputTokens, c.RepairCount, c.RetryCount, c.ErrorText); err != nil {
			t.Fatal(err)
		}
	}
	report, err := metrics.Read(ctx, job, owner)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "completed" || report.AttemptCount != 2 || len(report.Attempts) != 2 || len(report.Roles) != 3 || report.Attempts[1].WriterReadyMS != 1200 {
		t.Fatalf("incomplete metrics: %+v", report)
	}
	var repairs, retries, failures int64
	for _, r := range report.Roles {
		repairs += r.Repairs
		retries += r.RecordedRetries
		failures += r.Failures
	}
	if repairs != 1 || retries != 2 || failures != 1 {
		t.Fatal("recorded role counters lost")
	}
	raw, _ := json.Marshal(report)
	if strings.Contains(string(raw), "PRIVATE_ERROR_MUST_NOT_BE_EXPOSED") {
		t.Fatal("raw errors exposed")
	}
	foreign, _ := id.New()
	if _, err = metrics.Read(ctx, job, foreign); !errors.Is(err, generationmetrics.ErrNotFound) {
		t.Fatal("foreign metrics exposed")
	}
	if _, err = metrics.Read(ctx, job, ""); !errors.Is(err, generationmetrics.ErrNotFound) {
		t.Fatal("unscoped metrics exposed")
	}
}
