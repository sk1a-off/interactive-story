package generation

import (
	"bytes"
	"context"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

type updateMem struct {
	mu   sync.Mutex
	rows map[id.ID][]Update
}

func (m *updateMem) Append(_ context.Context, u Update) (Update, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows == nil {
		m.rows = map[id.ID][]Update{}
	}
	u.Sequence = int64(len(m.rows[u.GenerationID]) + 1)
	m.rows[u.GenerationID] = append(m.rows[u.GenerationID], u)
	return u, nil
}
func (m *updateMem) List(_ context.Context, g id.ID, after int64) ([]Update, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Update
	for _, u := range m.rows[g] {
		if u.Sequence > after {
			out = append(out, u)
		}
	}
	return out, nil
}
func TestPersistedSinkAssignsSequenceBeforeLivePublish(t *testing.T) {
	g := id.MustParse("00000000-0000-4000-8000-000000000801")
	store := &updateMem{}
	hub := NewHub()
	ch, cancel := hub.Subscribe(g, 4)
	defer cancel()
	sink := PersistedSink{Store: store, Live: hub}
	sink.Publish(context.Background(), Update{GenerationID: g, Phase: PhaseQueued})
	sink.Publish(context.Background(), Update{GenerationID: g, Phase: PhaseWriting})
	first := <-ch
	second := <-ch
	if first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("live stream lost persisted sequence: %d %d", first.Sequence, second.Sequence)
	}
	history, _ := store.List(context.Background(), g, 1)
	if len(history) != 1 || history[0].Sequence != 2 {
		t.Fatal("cursor replay returned wrong updates")
	}
}

// ctxStore rejects appends on a done context, like a real DB driver.
type ctxStore struct{ updateMem }

func (s *ctxStore) Append(ctx context.Context, u Update) (Update, error) {
	if err := ctx.Err(); err != nil {
		return u, err
	}
	return s.updateMem.Append(ctx, u)
}

func TestPersistedSinkPersistsTerminalUpdateOnCancelledContext(t *testing.T) {
	g := id.MustParse("00000000-0000-4000-8000-000000000802")
	store := &ctxStore{}
	var logs bytes.Buffer
	sink := PersistedSink{Store: store, Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sink.Publish(ctx, Update{GenerationID: g, Phase: PhaseWriting})
	sink.Publish(ctx, Update{GenerationID: g, Phase: PhaseFailed, Error: "timeout"})
	history, _ := store.List(context.Background(), g, 0)
	if len(history) != 1 || history[0].Phase != PhaseFailed {
		t.Fatalf("history = %+v, want only the terminal failed update persisted", history)
	}
	if !strings.Contains(logs.String(), "generation update persist failed") {
		t.Fatalf("append error of non-terminal update was not logged: %q", logs.String())
	}
}
