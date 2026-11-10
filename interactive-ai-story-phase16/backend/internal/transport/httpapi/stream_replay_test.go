package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	gen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

type streamReplayApp struct {
	history []gen.Update
	live    chan gen.Update
}

func (a streamReplayApp) Submit(context.Context, string, gen.PlayerAction) (id.ID, error) {
	return "", nil
}
func (a streamReplayApp) Cancel(context.Context, id.ID) error         { return nil }
func (a streamReplayApp) Subscribe(id.ID) (<-chan gen.Update, func()) { return a.live, func() {} }
func (a streamReplayApp) Replay(context.Context, id.ID, int64) ([]gen.Update, error) {
	return a.history, nil
}
func TestStreamingReplayOmitsTransientSSEIDsAndRejectsStaleDraft(t *testing.T) {
	gid := id.MustParse("00000000-0000-4000-8000-000000000019")
	app := streamReplayApp{history: []gen.Update{{GenerationID: gid, Sequence: 10, Phase: gen.PhaseWriting}, {GenerationID: gid, Phase: gen.PhaseDraftStreaming, Revision: 1000002, Provisional: true, TextDelta: "PREFIX"}, {GenerationID: gid, Sequence: 11, Phase: gen.PhaseDraftReady, Revision: 1000004, Provisional: true, TextDelta: "FINAL"}}, live: make(chan gen.Update, 2)}
	app.live <- gen.Update{GenerationID: gid, Phase: gen.PhaseDraftStreaming, Revision: 1000003, Provisional: true, TextDelta: "STALE_AFTER_READY"}
	app.live <- gen.Update{GenerationID: gid, Sequence: 12, Phase: gen.PhaseCompleted}
	router := chi.NewRouter()
	registerGenerationRoutes(router, app)
	request := httptest.NewRequest("GET", "/api/v1/generations/"+string(gid)+"/events", nil)
	request.Header.Set("Last-Event-ID", "10")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != 200 || strings.Contains(body, "id: 0") || strings.Contains(body, "STALE_AFTER_READY") || !strings.Contains(body, "PREFIX") || !strings.Contains(body, "id: 11") || !strings.Contains(body, "id: 12") {
		t.Fatalf("invalid transient replay: %s", body)
	}
	for _, block := range strings.Split(body, "\n\n") {
		if strings.Contains(block, "draft_streaming") && strings.Contains(block, "\nid:") {
			t.Fatal("transient frame overwrote durable SSE cursor")
		}
	}
}
