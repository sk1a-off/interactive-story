package generationupdates

import (
	"context"
	app "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

type Store interface {
	Append(context.Context, app.Update) (app.Update, error)
	List(context.Context, id.ID, int64) ([]app.Update, error)
}
