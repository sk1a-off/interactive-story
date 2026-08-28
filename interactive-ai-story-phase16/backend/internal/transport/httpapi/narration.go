package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	appnarration "github.com/local/interactive-ai-story/backend/internal/application/narration"
)

type NarrationApplication interface {
	Health(context.Context) (appnarration.Health, error)
	Speech(context.Context, string) ([]byte, string, error)
}

func registerNarrationRoutes(r interface {
	Get(string, http.HandlerFunc)
	Post(string, http.HandlerFunc)
}, app NarrationApplication) {
	r.Get("/api/v1/narration/health", func(w http.ResponseWriter, req *http.Request) {
		health, err := app.Health(req.Context())
		if err != nil {
			writeNarrationError(w, http.StatusServiceUnavailable, "omnivoice_unavailable", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(health)
	})
	r.Post("/api/v1/narration/speech", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, req.Body, 32<<10))
		if err := decoder.Decode(&body); err != nil {
			writeNarrationError(w, http.StatusBadRequest, "invalid_request", err)
			return
		}
		audio, contentType, err := app.Speech(req.Context(), body.Text)
		if err != nil {
			if errors.Is(err, appnarration.ErrInvalidText) {
				writeNarrationError(w, http.StatusBadRequest, "invalid_narration_text", err)
				return
			}
			writeNarrationError(w, http.StatusServiceUnavailable, "omnivoice_unavailable", err)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(audio)))
		w.Header().Set("Cache-Control", "private, no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(audio)
	})
}

func writeNarrationError(w http.ResponseWriter, status int, code string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": err.Error()})
}
