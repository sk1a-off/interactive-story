package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireImageWorkerToken(t *testing.T) {
	t.Parallel()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	tests := []struct {
		name       string
		token      string
		path       string
		authorize  string
		wantStatus int
	}{
		{name: "disabled", path: "/internal/image-worker/status", wantStatus: http.StatusNoContent},
		{name: "public route unaffected", token: "secret", path: "/health/live", wantStatus: http.StatusNoContent},
		{name: "missing token", token: "secret", path: "/internal/image-worker/status", wantStatus: http.StatusUnauthorized},
		{name: "wrong token", token: "secret", path: "/internal/image-worker/status", authorize: "Bearer wrong", wantStatus: http.StatusUnauthorized},
		{name: "valid token", token: "secret", path: "/internal/image-worker/status", authorize: "Bearer secret", wantStatus: http.StatusNoContent},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, test.path, nil)
			if test.authorize != "" {
				req.Header.Set("Authorization", test.authorize)
			}
			res := httptest.NewRecorder()
			RequireImageWorkerToken(next, test.token).ServeHTTP(res, req)
			if res.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
		})
	}
}
