package postgres_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
)

func TestImageSettingsMigrationRoundTrip(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := bootstrap.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Temporary tables shadow the real tables on this connection. Exercise the
	// actual migration SQL without altering user generations or global schema.
	exec := func(sql string) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TEMP TABLE image_generations (
 width integer NOT NULL CONSTRAINT image_generations_width_check CHECK(width=768),
 height integer NOT NULL CONSTRAINT image_generations_height_check CHECK(height=768),
 image_count smallint NOT NULL CONSTRAINT image_generations_image_count_check CHECK(image_count=2));
 CREATE TEMP TABLE scene_images (variant smallint NOT NULL CONSTRAINT scene_images_variant_check CHECK(variant IN (1,2)));
 INSERT INTO image_generations VALUES(768,768,2);
 INSERT INTO scene_images VALUES(1),(2);`)
	raw, err := os.ReadFile("../../db/migrations/00036_perchance_image_dimensions.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, ok := strings.Cut(string(raw), "-- +goose Down")
	if !ok {
		t.Fatal("migration has no down section")
	}
	exec(up)
	exec(down)
	exec(up)

	for _, resolution := range []string{"768,768", "512,768", "768,512"} {
		for _, count := range []string{"1", "2", "3", "4"} {
			exec("INSERT INTO image_generations VALUES(" + resolution + "," + count + ")")
		}
	}
	exec("INSERT INTO scene_images VALUES(3),(4)")
	reject := func(sql, message string) {
		t.Helper()
		savepoint, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = savepoint.Exec(ctx, sql)
		if err == nil || (message != "" && !strings.Contains(err.Error(), message)) {
			t.Fatalf("expected rejection containing %q; got %v", message, err)
		}
		if err := savepoint.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, values := range []string{"512,512,2", "1024,768,2", "768,768,0", "768,768,5"} {
		reject("INSERT INTO image_generations VALUES("+values+")", "check constraint")
	}
	for _, variant := range []string{"0", "5"} {
		reject("INSERT INTO scene_images VALUES("+variant+")", "check constraint")
	}
	reject(down, "Cannot roll back Perchance image settings")
	exec("DELETE FROM image_generations WHERE width<>768 OR height<>768 OR image_count<>2; DELETE FROM scene_images WHERE variant>2")
	exec(down)
	reject("INSERT INTO image_generations VALUES(512,768,2)", "check constraint")
	reject("INSERT INTO scene_images VALUES(3)", "check constraint")
	exec(up)
	var legacyCount int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM image_generations WHERE width=768 AND height=768 AND image_count=2").Scan(&legacyCount); err != nil || legacyCount == 0 {
		t.Fatalf("legacy generations were not preserved: count=%d error=%v", legacyCount, err)
	}
}
