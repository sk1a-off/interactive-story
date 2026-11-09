// promptbundleapply atomically clones the active prompt revision and replaces
// the tested Writer, state evaluator and world evaluator snapshots together.
package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	pg "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	app "github.com/local/interactive-ai-story/backend/internal/application/promptset"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
)

type replacement struct {
	role     string
	path     string
	wantHash string
	text     string
}

func main() {
	writer := flag.String("writer", "", "tested Writer prompt snapshot")
	writerHash := flag.String("writer-sha256", "", "required Writer SHA-256")
	state := flag.String("state", "", "tested state evaluator prompt snapshot")
	stateHash := flag.String("state-sha256", "", "required state SHA-256")
	world := flag.String("world", "", "tested world evaluator prompt snapshot")
	worldHash := flag.String("world-sha256", "", "required world SHA-256")
	expected := flag.Int64("expected-revision", 0, "required active revision")
	apply := flag.Bool("apply", false, "create and activate one new revision")
	flag.Parse()

	check := func(err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	replacements := []replacement{
		{role: "writer", path: *writer, wantHash: *writerHash},
		{role: "state_evaluator", path: *state, wantHash: *stateHash},
		{role: "world_evaluator", path: *world, wantHash: *worldHash},
	}
	if *expected < 1 {
		check(fmt.Errorf("expected revision is required"))
	}
	for index := range replacements {
		raw, err := os.ReadFile(replacements[index].path)
		check(err)
		actual := fmt.Sprintf("%x", sha256.Sum256(raw))
		if replacements[index].wantHash == "" || actual != replacements[index].wantHash || strings.TrimSpace(string(raw)) == "" {
			check(fmt.Errorf("%s snapshot SHA-256 mismatch", replacements[index].role))
		}
		replacements[index].text = string(raw)
		fmt.Printf("verified_role=%s snapshot_sha256=%s\n", replacements[index].role, actual)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://story:story@127.0.0.1:5432/story?sslmode=disable"
	}
	pool, err := bootstrap.OpenPostgres(ctx, dsn)
	check(err)
	defer pool.Close()
	repo := pg.NewPromptSetRepository(pool)
	before, err := repo.Active(ctx)
	check(err)
	if before.Revision != *expected {
		check(fmt.Errorf("active revision changed: got %d, expected %d", before.Revision, *expected))
	}
	fmt.Printf("previous_id=%s previous_revision=%d\n", before.ID, before.Revision)
	if !*apply {
		return
	}

	prompts := make(map[string]string, len(before.Prompts))
	for role, text := range before.Prompts {
		prompts[role] = text
	}
	for _, item := range replacements {
		prompts[item.role] = item.text
	}
	service := app.Service{Repo: repo}
	created, err := service.Create(ctx, app.CreateRequest{Prompts: prompts, RoleSettings: before.RoleSettings, Activate: true})
	check(err)
	after, err := repo.Active(ctx)
	check(err)
	if after.ID.String() != created.ID || !reflect.DeepEqual(after.Prompts, prompts) || !reflect.DeepEqual(after.RoleSettings, before.RoleSettings) {
		check(fmt.Errorf("post-activation verification failed; previous revision %s remains available", before.ID))
	}
	fmt.Printf("active_id=%s active_revision=%d changed_roles=writer,state_evaluator,world_evaluator settings_preserved=true\n", after.ID, after.Revision)
}
