package memoryruntimerepo

import (
	"context"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

type Profile struct {
	Revision                          int64
	ConfigID                          id.ID
	Signature, Provider, Model, State string
}
type Status struct {
	Enabled         bool   `json:"enabled"`
	State           string `json:"state"`
	Revision        int64  `json:"revision"`
	ServingRevision int64  `json:"servingRevision"`
	Queued          int64  `json:"queued"`
	Running         int64  `json:"running"`
	Completed       int64  `json:"completed"`
	Failed          int64  `json:"failed"`
}
type Repository interface {
	Ensure(context.Context, id.ID, string, string, string, bool) (Profile, error)
	Serving(context.Context, string) (Profile, error)
	Work(context.Context, string) ([]Profile, error)
	Finalize(context.Context, int64) error
	Status(context.Context, string) (Status, error)
}
