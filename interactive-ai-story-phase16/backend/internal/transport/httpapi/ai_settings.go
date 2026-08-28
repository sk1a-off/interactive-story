package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	app "github.com/local/interactive-ai-story/backend/internal/application/aiconfig"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

type AISettingsApplication interface {
	List(context.Context) ([]domain.SafeView, error)
	Active(context.Context) (domain.SafeView, error)
	Create(context.Context, app.UpdateRequest) (domain.SafeView, error)
	Activate(context.Context, id.ID) (domain.SafeView, error)
}

func registerAISettingsRoutes(r chi.Router, a AISettingsApplication) {
	r.Get("/api/v1/settings/ai", func(w http.ResponseWriter, req *http.Request) {
		v, e := a.List(req.Context())
		if e != nil {
			writeError(w, 500, "ai_settings_list_failed", e.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"revisions": v})
	})
	r.Get("/api/v1/settings/ai/active", func(w http.ResponseWriter, req *http.Request) {
		v, e := a.Active(req.Context())
		if e != nil {
			writeError(w, 500, "ai_settings_active_failed", e.Error())
			return
		}
		writeJSON(w, 200, v)
	})
	r.Post("/api/v1/settings/ai/revisions", func(w http.ResponseWriter, req *http.Request) {
		var body app.UpdateRequest
		dec := json.NewDecoder(req.Body)
		dec.DisallowUnknownFields()
		if e := dec.Decode(&body); e != nil {
			writeError(w, 400, "invalid_json", e.Error())
			return
		}
		v, e := a.Create(req.Context(), body)
		if e != nil {
			writeError(w, 422, "ai_settings_invalid", e.Error())
			return
		}
		writeJSON(w, 201, v)
	})
	r.Post("/api/v1/settings/ai/revisions/{revisionId}/activate", func(w http.ResponseWriter, req *http.Request) {
		rid, e := id.Parse(chi.URLParam(req, "revisionId"))
		if e != nil {
			writeError(w, 400, "invalid_revision_id", e.Error())
			return
		}
		v, e := a.Activate(req.Context(), rid)
		if e != nil {
			writeError(w, 422, "ai_settings_activate_failed", e.Error())
			return
		}
		writeJSON(w, 200, v)
	})
}
