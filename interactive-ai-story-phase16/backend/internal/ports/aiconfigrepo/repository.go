package aiconfigrepo

import (
	"context"
	"github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

type Repository interface {
	CreateRevision(context.Context, aiconfig.CreateRevisionCommand) (aiconfig.Revision, error)
	Get(context.Context, id.ID) (aiconfig.Revision, error)
	List(context.Context) ([]aiconfig.Revision, error)
	Active(context.Context) (aiconfig.Revision, error)
	Activate(context.Context, id.ID) error
}
