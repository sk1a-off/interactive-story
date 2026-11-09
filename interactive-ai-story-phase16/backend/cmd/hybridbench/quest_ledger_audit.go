package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

func auditQuestLedger(dsn, timelines, output string) error {
	if timelines == "" || output == "" {
		return fmt.Errorf("timeline and output required")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		return fmt.Errorf("output exists or inaccessible")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := bootstrap.OpenPostgres(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	type row struct {
		TimelineID string              `json:"timelineId"`
		Head       int64               `json:"head"`
		Counts     map[string]int      `json:"counts"`
		Records    []questLedgerRecord `json:"records,omitempty"`
		Error      string              `json:"error,omitempty"`
	}
	report := struct {
		CreatedAt time.Time `json:"createdAt"`
		Timelines []row     `json:"timelines"`
	}{CreatedAt: time.Now().UTC()}
	for _, text := range strings.Split(timelines, ",") {
		parsed, e := id.Parse(strings.TrimSpace(text))
		if e != nil {
			return e
		}
		tid := timeline.ID(parsed)
		item := row{TimelineID: string(tid), Counts: map[string]int{}}
		if err = tx.QueryRow(ctx, `SELECT head_event_seq FROM timelines WHERE id=$1`, tid).Scan(&item.Head); err != nil {
			return err
		}
		rows, e := tx.Query(ctx, `SELECT id,timeline_id,seq,event_type,schema_version,payload,generation_id FROM story_events WHERE timeline_id=$1 AND seq<=$2 ORDER BY seq`, tid, item.Head)
		if e != nil {
			return e
		}
		var source []event.StoredEvent
		for rows.Next() {
			var v event.StoredEvent
			if e = rows.Scan(&v.ID, &v.TimelineID, &v.Seq, &v.Type, &v.SchemaVersion, &v.Payload, &v.GenerationID); e != nil {
				rows.Close()
				return e
			}
			source = append(source, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		var objectives json.RawMessage
		if e = tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'progress',metadata->'progress','successCriteria',metadata->'successCriteria','status',CASE status WHEN 'closed' THEN 'completed' WHEN 'abandoned' THEN 'failed' ELSE 'active' END)),'[]'::jsonb) FROM story_threads WHERE timeline_id=$1 AND metadata->>'kind'='objective'`, tid).Scan(&objectives); e != nil {
			return e
		}
		input, _ := json.Marshal(map[string]any{"currentObjectives": objectives})
		enriched, e := questInputWithLedger(input, tid, item.Head, source)
		if e != nil {
			item.Error = e.Error()
		} else {
			var decoded struct {
				Ledger questLedgerContext `json:"questHistoryLedger"`
			}
			if e = json.Unmarshal(enriched, &decoded); e != nil {
				return e
			}
			item.Records = decoded.Ledger.Records
			for _, record := range item.Records {
				item.Counts[record.Provenance.Kind]++
			}
		}
		report.Timelines = append(report.Timelines, item)
		fmt.Printf("timeline=%s head=%d counts=%v error=%s\n", tid, item.Head, item.Counts, item.Error)
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(output, raw, 0600)
}
