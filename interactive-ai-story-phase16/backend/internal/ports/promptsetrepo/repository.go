package promptsetrepo

import (
	"context"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
)

type Repository interface {
	CreateRevision(context.Context, promptset.CreateCommand) (promptset.Revision, error)
	Get(context.Context, id.ID) (promptset.Revision, error)
	List(context.Context) ([]promptset.Revision, error)
	Active(context.Context) (promptset.Revision, error)
	Activate(context.Context, id.ID) error
}
