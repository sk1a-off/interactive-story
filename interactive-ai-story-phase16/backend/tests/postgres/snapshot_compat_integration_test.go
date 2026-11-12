package postgres_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	pgadapter "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/application/canon"
	"github.com/local/interactive-ai-story/backend/internal/application/saves"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/projection"
	"github.com/local/interactive-ai-story/backend/internal/domain/savepoint"
	"github.com/local/interactive-ai-story/backend/internal/domain/snapshot"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/repositories"
)

// Snapshots written by the legacy serializer (exact struct JSON hash) must keep
// loading and forking after the state type evolves, and new snapshots must be
// written with the canonical serializer.
func TestLegacySerializerSnapshotRestoresAndForks(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := bootstrap.OpenPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	ownerRaw, _ := id.New()
	storyRaw, _ := id.New()
	parentRaw, _ := id.New()
	ownerID := story.UserID(ownerRaw)
	storyID := story.ID(storyRaw)
	parentID := timeline.ID(parentRaw)
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,username) VALUES ($1,$2)`, ownerRaw, "legacy-"+ownerRaw.String()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	st, _ := story.New(storyID, ownerID, "Legacy Snapshot Story", now)
	parent, _ := timeline.New(parentID, storyID, "Main")
	uow := pgadapter.NewUnitOfWork(pool)
	if err := uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		if err := repos.Stories().Create(ctx, st); err != nil {
			return err
		}
		return repos.Timelines().Create(ctx, parent)
	}); err != nil {
		t.Fatal(err)
	}
	chapterRaw, _ := id.New()
	if _, err := pool.Exec(ctx, `INSERT INTO chapters(id,timeline_id,number,title,status) VALUES($1,$2,1,'Opening','active')`, chapterRaw, parentID); err != nil {
		t.Fatal(err)
	}
	canonSvc := canon.NewService(uow, func() time.Time { return now })
	if _, err := canonSvc.AppendSemantic(ctx, parentID, 0, []event.PendingEvent{{Type: "player_action_attempted", SchemaVersion: 1, Payload: json.RawMessage(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	saveSvc := saves.NewService(uow, func() time.Time { return now }, nil)
	saved, err := saveSvc.Create(ctx, saves.CreateCommand{TimelineID: parentID, Name: "Legacy", Kind: savepoint.KindManual})
	if err != nil {
		t.Fatal(err)
	}

	var serializer int
	if err := pool.QueryRow(ctx, `SELECT serializer_version FROM snapshots WHERE id=$1`, saved.SnapshotID).Scan(&serializer); err != nil {
		t.Fatal(err)
	}
	if serializer != snapshot.SerializerCanonical {
		t.Fatalf("new snapshots must use the canonical serializer, got %d", serializer)
	}

	// Rewrite the row the way an older binary would have persisted it: the
	// exact struct JSON of a state type that did not yet have the
	// DirectorInstructions collection, hashed byte-for-byte.
	state, err := canonSvc.Rebuild(ctx, parentID)
	if err != nil {
		t.Fatal(err)
	}
	legacyJSON, err := projection.CanonicalJSON(state)
	if err != nil {
		t.Fatal(err)
	}
	trimmed := bytes.Replace(legacyJSON, []byte(`,"DirectorInstructions":{}`), nil, 1)
	if bytes.Equal(trimmed, legacyJSON) {
		t.Fatalf("fixture assumption broken: DirectorInstructions not found in %s", legacyJSON)
	}
	legacySum := sha256.Sum256(trimmed)
	legacyHash := hex.EncodeToString(legacySum[:])
	if _, err := pool.Exec(ctx, `UPDATE snapshots SET state=$2, state_hash=$3, serializer_version=$4 WHERE id=$1`, saved.SnapshotID, trimmed, legacyHash, snapshot.SerializerLegacy); err != nil {
		t.Fatal(err)
	}

	var restored snapshot.Snapshot
	if err := uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		var err error
		restored, err = repos.Snapshots().Get(ctx, snapshot.ID(saved.SnapshotID))
		return err
	}); err != nil {
		t.Fatalf("legacy snapshot failed to load: %v", err)
	}
	if restored.SerializerVersion != snapshot.SerializerCanonical || restored.SchemaVersion != projection.SchemaVersion || len(restored.State.Narrative.Chapters) != 1 {
		t.Fatalf("legacy snapshot not restored correctly: %+v", restored)
	}
	if err := pool.QueryRow(ctx, `SELECT serializer_version FROM snapshots WHERE id=$1`, saved.SnapshotID).Scan(&serializer); err != nil {
		t.Fatal(err)
	}
	if serializer != snapshot.SerializerLegacy {
		t.Fatal("restore must not rewrite the persisted snapshot row")
	}

	childRaw, _ := id.New()
	child, err := saveSvc.Fork(ctx, saves.ForkCommand{SaveID: saved.ID, TimelineID: timeline.ID(childRaw), Name: "From legacy"})
	if err != nil {
		t.Fatalf("fork from legacy snapshot failed: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT serializer_version FROM snapshots WHERE id=$1`, child.HeadSnapshotID).Scan(&serializer); err != nil {
		t.Fatal(err)
	}
	if serializer != snapshot.SerializerCanonical {
		t.Fatalf("forked snapshot must be canonical, got %d", serializer)
	}
	var childChapters int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM chapters WHERE timeline_id=$1`, childRaw).Scan(&childChapters); err != nil {
		t.Fatal(err)
	}
	if childChapters != 1 {
		t.Fatalf("fork lost narrative state: chapters=%d", childChapters)
	}

	// A legacy row carrying data the current type cannot decode must be refused.
	if _, err := pool.Exec(ctx, `UPDATE snapshots SET state=jsonb_set(state,'{narrative,RetiredCollection}','{"x":1}'::jsonb) WHERE id=$1`, saved.SnapshotID); err != nil {
		t.Fatal(err)
	}
	err = uow.WithinTransaction(ctx, func(ctx context.Context, repos repositories.CanonRepositories) error {
		_, err := repos.Snapshots().Get(ctx, snapshot.ID(saved.SnapshotID))
		return err
	})
	if err == nil {
		t.Fatal("snapshot with undecodable data was accepted")
	}
}
