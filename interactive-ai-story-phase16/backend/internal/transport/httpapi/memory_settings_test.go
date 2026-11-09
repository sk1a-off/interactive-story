package httpapi

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/local/interactive-ai-story/backend/internal/application/memoryruntime"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/memoryruntimerepo"
	"net/http/httptest"
	"strings"
	"testing"
)

type memorySettingsStub struct {
	err   error
	calls int
}

func (s *memorySettingsStub) MemoryStatus(context.Context) (repo.Status, error) {
	return repo.Status{State: "building"}, s.err
}
func (s *memorySettingsStub) MemoryReindex(context.Context) (repo.Status, error) {
	s.calls++
	return repo.Status{State: "building"}, s.err
}
func TestMemoryRoutesRejectBadRequestsAndHideInternalErrors(t *testing.T) {
	for _, test := range []struct {
		method, path, body, content string
		err                         error
		want, calls                 int
	}{
		{"GET", "/api/v1/settings/memory", "", "", nil, 200, 0},
		{"GET", "/api/v1/settings/memory", "", "", errors.New("SECRET"), 503, 0},
		{"POST", "/api/v1/settings/memory/reindex", "{}", "application/json", nil, 202, 1},
		{"POST", "/api/v1/settings/memory/reindex", "{}", "application/json", memoryruntime.ErrDisabled, 422, 1},
		{"POST", "/api/v1/settings/memory/reindex", "{}", "application/json", errors.New("SECRET"), 503, 1},
		{"POST", "/api/v1/settings/memory/reindex", "{", "application/json", nil, 400, 0},
		{"POST", "/api/v1/settings/memory/reindex", "{}", "text/plain", nil, 415, 0},
	} {
		a := &memorySettingsStub{err: test.err}
		r := chi.NewRouter()
		registerMemorySettingsRoutes(r, a)
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		req.Header.Set("Content-Type", test.content)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != test.want || a.calls != test.calls || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatalf("status=%d calls=%d body=%s", w.Code, a.calls, w.Body.String())
		}
	}
}
