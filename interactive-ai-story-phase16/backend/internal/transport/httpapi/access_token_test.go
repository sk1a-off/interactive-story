package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
}

func TestRequireAccessTokenDisabledWhenEmpty(t *testing.T) {
	h := RequireAccessToken(okHandler(), "", "")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/stories", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("empty token must not enforce auth, got %d", rec.Code)
	}
}

func TestRequireAccessTokenGuardsEverythingExceptHealth(t *testing.T) {
	h := RequireAccessToken(okHandler(), "secret-token", "")
	for _, path := range []string{"/api/v1/stories", "/api/v1/generations/x/events", "/media/stories/a.jpg", "/internal/image-worker/jobs/next"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s without token: got %d want 401", path, rec.Code)
		}
	}
	for _, path := range []string{"/health", "/health/live", "/health/ready"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s must stay public, got %d", path, rec.Code)
		}
	}
	bearer := httptest.NewRequest(http.MethodPost, "/api/v1/timelines/x/actions", nil)
	bearer.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, bearer)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid bearer rejected: %d", rec.Code)
	}
	wrong := httptest.NewRequest(http.MethodPost, "/api/v1/timelines/x/actions", nil)
	wrong.Header.Set("Authorization", "Bearer secret-tokeN")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, wrong)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong bearer accepted: %d", rec.Code)
	}
}

func TestRequireAccessTokenSessionCookieFlow(t *testing.T) {
	h := RequireAccessToken(okHandler(), "secret-token", "")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/session?token=nope", nil))
	if rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("invalid login token must not set a cookie: %d %v", rec.Code, rec.Result().Cookies())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/session?token=secret-token&next=/reader/abc", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/reader/abc" {
		t.Fatalf("login should redirect to next: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != AccessCookieName || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected session cookie: %+v", cookies)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/session?token=secret-token&next=https://evil.example/", nil))
	if rec.Header().Get("Location") != "/" {
		t.Fatalf("open redirect accepted: %q", rec.Header().Get("Location"))
	}
	sse := httptest.NewRequest(http.MethodGet, "/api/v1/generations/x/events", nil)
	sse.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, sse)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie session rejected: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/auth/session", nil))
	if rec.Code != http.StatusNoContent || len(rec.Result().Cookies()) != 1 || rec.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("logout did not clear the cookie: %d %v", rec.Code, rec.Result().Cookies())
	}
}

func TestRequireAccessTokenDelegatesWorkerRoutesWhenWorkerTokenConfigured(t *testing.T) {
	inner := RequireImageWorkerToken(okHandler(), "worker-token")
	h := RequireAccessToken(inner, "secret-token", "worker-token")
	req := httptest.NewRequest(http.MethodGet, "/internal/image-worker/jobs/next", nil)
	req.Header.Set("Authorization", "Bearer worker-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("worker token must open worker routes: %d", rec.Code)
	}
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("api token must not open worker routes: %d", rec.Code)
	}
	api := httptest.NewRequest(http.MethodGet, "/api/v1/stories", nil)
	api.Header.Set("Authorization", "Bearer worker-token")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, api)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("worker token must not open api routes: %d", rec.Code)
	}
}

func TestMediaHandlerServesFilesButNotDirectoryListings(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "stories", "s1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "stories", "s1", "1.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := http.StripPrefix("/media/", MediaHandler(root))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/stories/s1/1.png", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "png" {
		t.Fatalf("file not served: %d %q", rec.Code, rec.Body.String())
	}
	for _, path := range []string{"/media/", "/media/stories/", "/media/stories/s1/"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: directory listing exposed (%d)", path, rec.Code)
		}
	}
}

func TestAccessSessionResponsesAreNotCachedOrReferred(t *testing.T) {
	h := RequireAccessToken(okHandler(), "secret-token", "")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/session?token=secret-token", nil))
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("login response must not be cached or leak its URL: %v", rec.Header())
	}
}
