package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	app "github.com/local/interactive-ai-story/backend/internal/application/promptset"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/promptset"
)

type PromptSettingsApplication interface {
	Studio(context.Context) (app.StudioView, error)
	Create(context.Context, app.CreateRequest) (domain.SafeView, error)
	Activate(context.Context, id.ID) (domain.SafeView, error)
}

func registerPromptSettingsRoutes(r chi.Router, a PromptSettingsApplication) {
	r.Get("/api/v1/settings/prompts", func(w http.ResponseWriter, req *http.Request) {
		view, err := a.Studio(req.Context())
		if err != nil {
			writeError(w, 500, "prompt_settings_failed", err.Error())
			return
		}
		writeJSON(w, 200, view)
	})
	r.Post("/api/v1/settings/prompts/revisions", func(w http.ResponseWriter, req *http.Request) {
		var body app.CreateRequest
		dec := json.NewDecoder(req.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			writeError(w, 400, "invalid_json", err.Error())
			return
		}
		view, err := a.Create(req.Context(), body)
		if err != nil {
			writeError(w, 422, "prompt_settings_invalid", err.Error())
			return
		}
		writeJSON(w, 201, view)
	})
	r.Post("/api/v1/settings/prompts/revisions/{revisionId}/activate", func(w http.ResponseWriter, req *http.Request) {
		revisionID, err := id.Parse(chi.URLParam(req, "revisionId"))
		if err != nil {
			writeError(w, 400, "invalid_revision_id", err.Error())
			return
		}
		view, err := a.Activate(req.Context(), revisionID)
		if err != nil {
			writeError(w, 422, "prompt_settings_activate_failed", err.Error())
			return
		}
		writeJSON(w, 200, view)
	})
}
