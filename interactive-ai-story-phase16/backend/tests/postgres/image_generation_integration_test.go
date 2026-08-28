package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	pgadapter "github.com/local/interactive-ai-story/backend/internal/adapters/postgres"
	"github.com/local/interactive-ai-story/backend/internal/bootstrap"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/imagegeneration"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/imagegenerationrepo"
)

func TestImageGenerationAnchorClaimIsAtomicAndFailedAnchorIsReusable(t *testing.T) {
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

	ownerID, _ := id.New()
	storyID, _ := id.New()
	timelineID, _ := id.New()
	chapterID, _ := id.New()
	sceneID, _ := id.New()
	beatID, _ := id.New()
	if _, err = pool.Exec(ctx, `INSERT INTO users(id,username) VALUES($1,$2)`, ownerID, "image-anchor-"+ownerID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO stories(id,owner_id,title) VALUES($1,$2,'Image anchor integration')`, storyID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO timelines(id,story_id,name) VALUES($1,$2,'Main')`, timelineID, storyID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO chapters(id,timeline_id,number,title,status) VALUES($1,$2,1,'Opening','active')`, chapterID, timelineID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO scenes(id,chapter_id,number,mood,goal,status) VALUES($1,$2,1,'night','reach the tower','active')`, sceneID, chapterID); err != nil {
		t.Fatal(err)
	}
	text := "Над затопленной площадью медленно поднялся огромный механический маяк, и холодный синий свет отразился в окнах, воде и лицах замерших людей."
	if _, err = pool.Exec(ctx, `INSERT INTO beats(id,scene_id,position,kind,content,status,is_active) VALUES($1,$2,1,'description',jsonb_build_object('text',$3::text),'committed',true)`, beatID, sceneID, text); err != nil {
		t.Fatal(err)
	}
	var configID id.ID
	if err = pool.QueryRow(ctx, `SELECT config_revision_id FROM ai_active_config WHERE singleton=true`).Scan(&configID); err != nil {
		t.Fatal(err)
	}

	imageRepo := pgadapter.NewImageGenerationRepository(pool)
	provider := ai.ProviderIdentity{Kind: ai.KindImage, Provider: "perchance_browser", Model: "text-to-image-plugin", Profile: "digital-painting-768x768x2"}
	newGeneration := func() domain.Generation {
		generationID, _ := id.New()
		generation, createErr := domain.New(generationID, storyID, sceneID, configID, &beatID, provider, "cinematic test prompt", "text", domain.Manual, time.Now().UTC())
		if createErr != nil {
			t.Fatal(createErr)
		}
		generation.AnchorParagraph = 1
		return generation
	}

	results := make(chan error, 2)
	for range 2 {
		generation := newGeneration()
		go func() { results <- imageRepo.Create(ctx, generation) }()
	}
	first, second := <-results, <-results
	if (first == nil) == (second == nil) {
		t.Fatalf("exactly one concurrent anchor claim must succeed: first=%v second=%v", first, second)
	}
	conflict := first
	if conflict == nil {
		conflict = second
	}
	if !errors.Is(conflict, repo.ErrAnchorOccupied) {
		t.Fatalf("duplicate anchor returned the wrong error: %v", conflict)
	}

	sources, err := imageRepo.LoadSceneSources(ctx, storyID, sceneID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || len(sources[0].ExcludedParagraphs) != 1 || sources[0].ExcludedParagraphs[0] != 1 {
		t.Fatalf("occupied anchor was not loaded for the visual director: %#v", sources)
	}
	if _, err = pool.Exec(ctx, `UPDATE image_generations SET status='failed',finished_at=now(),error_code='test' WHERE story_id=$1`, storyID); err != nil {
		t.Fatal(err)
	}
	if err = imageRepo.Create(ctx, newGeneration()); err != nil {
		t.Fatalf("failed anchor should be reusable: %v", err)
	}
}
