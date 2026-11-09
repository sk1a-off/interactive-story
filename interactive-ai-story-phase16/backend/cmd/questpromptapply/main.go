// questpromptapply clones the active prompt revision, changing only the tested
// role. It never opens provider credentials or generates a story.
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
	domain "github.com/local/interactive-ai-story/backend/internal/domain/promptset"
)

func main() {
	path := flag.String("prompt", "", "tested prompt snapshot")
	role := flag.String("role", "quest_evaluator", "role to replace: quest_evaluator, state_evaluator, director_editor or turn_planner")
	wantHash := flag.String("sha256", "", "required snapshot SHA-256")
	expected := flag.Int64("expected-revision", 0, "required active revision")
	apply := flag.Bool("apply", false, "create and activate a new revision")
	flag.Parse()
	if *role != "quest_evaluator" && *role != "director_editor" && *role != "state_evaluator" && *role != "turn_planner" {
		fmt.Fprintln(os.Stderr, "unsupported role")
		os.Exit(1)
	}
	check := func(err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	raw, err := os.ReadFile(*path)
	check(err)
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	if *expected < 1 || *wantHash == "" || hash != *wantHash || strings.TrimSpace(string(raw)) == "" {
		check(fmt.Errorf("missing expected revision or snapshot hash mismatch"))
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
	fmt.Printf("previous_id=%s previous_revision=%d snapshot_sha256=%s\n", before.ID, before.Revision, hash)
	if !*apply {
		return
	}
	if before.Prompts[*role] == string(raw) {
		fmt.Println("already_applied=true")
		return
	}
	prompts := make(map[string]string, len(before.Prompts))
	for role, text := range before.Prompts {
		prompts[role] = text
	}
	prompts[*role] = string(raw)
	settings := make(map[string]domain.RoleSettings, len(before.RoleSettings)+1)
	for role, value := range before.RoleSettings {
		settings[role] = value
	}
	if _, ok := settings[*role]; !ok {
		settings[*role] = domain.DefaultRoleSettings(*role)
	}
	service := app.Service{Repo: repo}
	created, err := service.Create(ctx, app.CreateRequest{Prompts: prompts, RoleSettings: settings, Activate: true})
	check(err)
	after, err := repo.Active(ctx)
	check(err)
	if after.ID.String() != created.ID || !reflect.DeepEqual(after.Prompts, prompts) || !reflect.DeepEqual(after.RoleSettings, settings) {
		check(fmt.Errorf("post-activation verification failed; previous revision %s remains available", before.ID))
	}
	fmt.Printf("active_id=%s active_revision=%d changed_role=%s settings_preserved=true\n", after.ID, after.Revision, *role)
}
