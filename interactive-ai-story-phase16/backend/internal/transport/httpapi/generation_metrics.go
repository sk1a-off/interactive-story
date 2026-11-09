package httpapi

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationmetrics"
	"net/http"
	"time"
)

type generationMetricsApplication interface {
	GenerationMetrics(context.Context, id.ID) (generationmetrics.Report, error)
}

func registerGenerationMetricsRoute(r chi.Router, a generationMetricsApplication) {
	r.Get("/api/v1/generations/{generationId}/metrics", func(w http.ResponseWriter, req *http.Request) {
		gid, err := id.Parse(chi.URLParam(req, "generationId"))
		if err != nil {
			writeError(w, 400, "invalid_generation_id", "Некорректный идентификатор генерации")
			return
		}
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		report, err := a.GenerationMetrics(ctx, gid)
		if err != nil {
			if errors.Is(err, generationmetrics.ErrNotFound) {
				writeError(w, 404, "generation_not_found", "Генерация не найдена")
			} else {
				writeError(w, 503, "generation_metrics_unavailable", "Замеры генерации недоступны")
			}
			return
		}
		writeJSON(w, 200, report)
	})
}
