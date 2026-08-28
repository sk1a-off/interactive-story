package tx

import (
	"context"

	"github.com/local/interactive-ai-story/backend/internal/ports/repositories"
)

type UnitOfWork interface {
	WithinTransaction(context.Context, func(context.Context, repositories.CanonRepositories) error) error
}
