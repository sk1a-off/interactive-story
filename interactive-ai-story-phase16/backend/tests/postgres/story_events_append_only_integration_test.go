package postgres_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

// Story Canon rows must never be rewritten or removed by ordinary code paths;
// only an explicit maintenance session may do so.
func TestStoryEventsAreAppendOnly(t *testing.T) {
	ctx, tx, _, tid := memoryOutboxFixture(t)
	eventID, _ := id.New()
	if _, err := tx.Exec(ctx, `INSERT INTO story_events(id,timeline_id,seq,event_type,schema_version,payload) VALUES($1,$2,3,'player_action_attempted',1,'{}')`, eventID, tid); err != nil {
		t.Fatal(err)
	}
	expectBlocked := func(label, sql string) {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = sp.Exec(ctx, sql, eventID)
		_ = sp.Rollback(ctx)
		var pgErr *pgconn.PgError
		if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "23000" {
			t.Fatalf("%s must be rejected as an integrity violation, got %v", label, err)
		}
	}
	expectBlocked("UPDATE", `UPDATE story_events SET payload='{"rewritten":true}' WHERE id=$1`)
	expectBlocked("DELETE", `DELETE FROM story_events WHERE id=$1`)

	// Explicit maintenance override, scoped to the transaction.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `SET LOCAL story.allow_event_rewrite = 'on'`); err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `UPDATE story_events SET payload='{"repaired":true}' WHERE id=$1`, eventID); err != nil {
		t.Fatalf("maintenance rewrite must be allowed: %v", err)
	}
	if err := sp.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := tx.QueryRow(ctx, `SELECT payload::text FROM story_events WHERE id=$1`, eventID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != `{"repaired": true}` {
		t.Fatalf("maintenance rewrite not applied: %s", payload)
	}
	// SET LOCAL lasts until the enclosing transaction ends, so reset it and
	// confirm protection is back.
	if _, err := tx.Exec(ctx, `RESET story.allow_event_rewrite`); err != nil {
		t.Fatal(err)
	}
	expectBlocked("UPDATE after reset", `UPDATE story_events SET payload='{}' WHERE id=$1`)
}
