package httpapi

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationmetrics"
	"net/http/httptest"
	"strings"
	"testing"
)

type generationMetricsStub struct {
	err   error
	calls int
}

func (s *generationMetricsStub) GenerationMetrics(context.Context, id.ID) (generationmetrics.Report, error) {
	s.calls++
	return generationmetrics.Report{Status: "running", Attempts: []generationmetrics.AttemptTiming{}}, s.err
}
func TestGenerationMetricsRouteScopesErrorsAndRejectsInvalidID(t *testing.T) {
	for _, test := range []struct {
		id            string
		err           error
		status, calls int
	}{
		{"bad", nil, 400, 0}, {"00000000-0000-4000-8000-000000000001", nil, 200, 1}, {"00000000-0000-4000-8000-000000000001", generationmetrics.ErrNotFound, 404, 1}, {"00000000-0000-4000-8000-000000000001", errors.New("PRIVATE_DATABASE_ERROR"), 503, 1},
	} {
		s := &generationMetricsStub{err: test.err}
		r := chi.NewRouter()
		registerGenerationMetricsRoute(r, s)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/generations/"+test.id+"/metrics", nil))
		if w.Code != test.status || s.calls != test.calls || strings.Contains(w.Body.String(), "PRIVATE_DATABASE_ERROR") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
}
