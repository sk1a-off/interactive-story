package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoints(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/health/live", "/health/ready"} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			res := httptest.NewRecorder()
			NewRouter().ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
			}
		})
	}
}
