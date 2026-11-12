package httpapi

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// AccessCookieName carries the API access token for browser sessions.
	// EventSource cannot send an Authorization header, so SSE relies on it.
	AccessCookieName  = "story_access"
	accessSessionPath = "/api/v1/auth/session"
	accessCookieTTL   = 30 * 24 * time.Hour
)

// RequireAccessToken protects every route except health checks with the
// configured API access token. Requests may present it as a Bearer token or
// through the session cookie issued by GET /api/v1/auth/session?token=…
// Image-worker routes keep their own token check when one is configured.
// An empty token disables the middleware (loopback single-user default).
func RequireAccessToken(next http.Handler, configuredToken, imageWorkerToken string) http.Handler {
	token := strings.TrimSpace(configuredToken)
	if token == "" {
		return next
	}
	workerProtected := strings.TrimSpace(imageWorkerToken) != ""
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/health" || strings.HasPrefix(path, "/health/"):
			next.ServeHTTP(w, r)
			return
		case path == accessSessionPath:
			handleAccessSession(w, r, token)
			return
		case workerProtected && strings.HasPrefix(path, imageWorkerPathPrefix):
			next.ServeHTTP(w, r)
			return
		}
		if !accessTokenPresented(r, token) {
			writeError(w, http.StatusUnauthorized, "access_token_required", "A valid API access token is required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func accessTokenPresented(r *http.Request, token string) bool {
	if provided, ok := strings.CutPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer "); ok {
		return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(provided)), []byte(token)) == 1
	}
	if cookie, err := r.Cookie(AccessCookieName); err == nil {
		return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(token)) == 1
	}
	return false
}

// handleAccessSession turns a one-time token presentation into a cookie
// session: GET/POST with ?token= sets the cookie and redirects to the
// application root; DELETE clears it.
func handleAccessSession(w http.ResponseWriter, r *http.Request, token string) {
	switch r.Method {
	case http.MethodGet, http.MethodPost:
		provided := strings.TrimSpace(r.URL.Query().Get("token"))
		if provided == "" {
			if p, ok := strings.CutPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer "); ok {
				provided = strings.TrimSpace(p)
			}
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			writeError(w, http.StatusUnauthorized, "access_token_invalid", "The access token is not valid.")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: AccessCookieName, Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: int(accessCookieTTL / time.Second)})
		redirect := "/"
		if next := r.URL.Query().Get("next"); next != "" {
			if parsed, err := url.Parse(next); err == nil && parsed.Scheme == "" && parsed.Host == "" && strings.HasPrefix(parsed.Path, "/") && !strings.HasPrefix(parsed.Path, "//") {
				redirect = next
			}
		}
		http.Redirect(w, r, redirect, http.StatusSeeOther)
	case http.MethodDelete:
		http.SetCookie(w, &http.Cookie{Name: AccessCookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Unsupported method.")
	}
}

// MediaHandler serves generated files below root without directory listings.
func MediaHandler(root string) http.Handler {
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}
