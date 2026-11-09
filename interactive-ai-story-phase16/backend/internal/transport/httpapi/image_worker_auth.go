package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

const imageWorkerPathPrefix = "/internal/image-worker/"

func RequireImageWorkerToken(next http.Handler, configuredToken string) http.Handler {
	token := strings.TrimSpace(configuredToken)
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, imageWorkerPathPrefix) {
			next.ServeHTTP(w, r)
			return
		}
		provided, ok := strings.CutPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			writeError(w, http.StatusUnauthorized, "image_worker_unauthorized", "A valid image worker token is required.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
