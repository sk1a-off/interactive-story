package aiconfig

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	domain "github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

func TestGenerationProfileRevisionIsPinnedAndDefaultsOnRead(t *testing.T) {
	ctx := context.Background()
	rp := &repo{}
	svc := Service{Repo: rp}
	cmd := command("local")
	cmd.Public.GenerationProfile = domain.GenerationBalance
	balanced, err := svc.Create(ctx, UpdateRequest{Config: cmd, Activate: true})
	if err != nil || balanced.Public.GenerationProfile != domain.GenerationBalance {
		t.Fatalf("balance not persisted: %v", err)
	}
	cmd.Public.GenerationProfile = domain.GenerationQuality
	quality, err := svc.Create(ctx, UpdateRequest{Config: cmd, Activate: true})
	if err != nil || quality.Public.GenerationProfile != domain.GenerationQuality {
		t.Fatalf("quality not persisted: %v", err)
	}
	old, _ := rp.Get(ctx, id.MustParse(balanced.ID))
	var pinned domain.PublicSettings
	_ = json.Unmarshal(old.Settings, &pinned)
	if pinned.GenerationProfile != domain.GenerationBalance || rp.active != id.MustParse(quality.ID) {
		t.Fatal("switching active config changed the old revision")
	}
	// Settings created before profiles have no field and keep quality semantics.
	rp.rows[0].Settings = json.RawMessage(`{}`)
	legacy := svc.safe(ctx, rp.rows[0], false)
	if legacy.Public.GenerationProfile != domain.GenerationQuality {
		t.Fatal("old revision did not default to quality")
	}
}

func TestUnavailableProfileCannotBeCreatedOrActivated(t *testing.T) {
	ctx := context.Background()
	rp := &repo{}
	svc := Service{Repo: rp}
	for _, profile := range []domain.GenerationProfile{domain.GenerationSpeed, "unknown"} {
		cmd := command("local")
		cmd.Public.GenerationProfile = profile
		if _, err := svc.Create(ctx, UpdateRequest{Config: cmd, Activate: true}); !errors.Is(err, domain.ErrInvalidPublicSettings) || len(rp.rows) != 0 {
			t.Fatalf("invalid profile created a revision: %v", err)
		}
	}
	// Defense for revisions inserted externally or by a future/downgraded server.
	v, _ := rp.CreateRevision(ctx, command("local"))
	rp.rows[0].Settings = json.RawMessage(`{"generationProfile":"speed"}`)
	if _, err := svc.Activate(ctx, v.ID); !errors.Is(err, domain.ErrInvalidPublicSettings) || !rp.active.IsZero() {
		t.Fatalf("unimplemented speed profile activated: %v", err)
	}
}
