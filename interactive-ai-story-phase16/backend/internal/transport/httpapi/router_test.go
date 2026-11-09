package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type readinessStub struct {
	ping func(context.Context) error
}

func (s readinessStub) Ping(ctx context.Context) error { return s.ping(ctx) }

func TestLiveHealthDoesNotDependOnReadiness(t *testing.T) {
	t.Parallel()
	checker := readinessStub{ping: func(context.Context) error {
		t.Fatal("liveness must not ping dependencies")
		return nil
	}}
	res := httptest.NewRecorder()
	newHealthRouter(checker).ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"status":"live"`) {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
}

func TestReadyHealthPingsDatabase(t *testing.T) {
	t.Parallel()
	called := false
	checker := readinessStub{ping: func(context.Context) error {
		called = true
		return nil
	}}
	res := httptest.NewRecorder()
	newHealthRouter(checker).ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if res.Code != http.StatusOK || !called || !strings.Contains(res.Body.String(), `"status":"ready"`) {
		t.Fatalf("called=%v status=%d body=%s", called, res.Code, res.Body.String())
	}
}

func TestReadyHealthHidesDatabaseError(t *testing.T) {
	t.Parallel()
	checker := readinessStub{ping: func(context.Context) error { return errors.New("secret database detail") }}
	res := httptest.NewRecorder()
	newHealthRouter(checker).ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if res.Code != http.StatusServiceUnavailable || !strings.Contains(res.Body.String(), `"status":"not_ready"`) || strings.Contains(res.Body.String(), "secret") {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
}

func TestReadinessHandlerTimesOut(t *testing.T) {
	t.Parallel()
	checker := readinessStub{ping: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	res := httptest.NewRecorder()
	readinessHandler(checker, 5*time.Millisecond).ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
}

func newHealthRouter(checker ReadinessChecker) http.Handler {
	return NewRouterWithApplicationsAndReadiness(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, checker)
}
