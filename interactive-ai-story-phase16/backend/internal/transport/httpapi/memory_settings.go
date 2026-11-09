package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/local/interactive-ai-story/backend/internal/application/memoryruntime"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/memoryruntimerepo"
)

type memorySettingsApplication interface {
	MemoryStatus(context.Context) (repo.Status, error)
	MemoryReindex(context.Context) (repo.Status, error)
}

func registerMemorySettingsRoutes(r chi.Router, a memorySettingsApplication) {
	r.Get("/api/v1/settings/memory", func(w http.ResponseWriter, req *http.Request) {
		status, err := a.MemoryStatus(req.Context())
		if err != nil {
			writeError(w, 503, "memory_status_unavailable", "Статус памяти недоступен")
			return
		}
		writeJSON(w, 200, status)
	})
	r.Post("/api/v1/settings/memory/reindex", func(w http.ResponseWriter, req *http.Request) {
		var body struct{}
		if !decodeStrictJSONRequest(w, req, &body) {
			return
		}
		status, err := a.MemoryReindex(req.Context())
		if err != nil {
			if errors.Is(err, memoryruntime.ErrDisabled) {
				writeError(w, 422, "memory_disabled", "Сначала включите память и сохраните настройки")
			} else {
				writeError(w, 503, "memory_reindex_unavailable", "Не удалось запустить переиндексацию памяти")
			}
			return
		}
		writeJSON(w, 202, status)
	})
}
