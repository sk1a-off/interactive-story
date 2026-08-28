package generation

import (
	"context"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

type UpdateStore interface {
	Append(context.Context, Update) (Update, error)
	List(context.Context, id.ID, int64) ([]Update, error)
}
type PersistedSink struct {
	Store UpdateStore
	Live  *Hub
}

func (s PersistedSink) Publish(ctx context.Context, u Update) {
	if s.Store != nil {
		stored, e := s.Store.Append(ctx, u)
		if e == nil {
			u = stored
		}
	}
	if s.Live != nil {
		s.Live.Publish(ctx, u)
	}
}
