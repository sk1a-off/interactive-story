package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	appdir "github.com/local/interactive-ai-story/backend/internal/application/director"
	domaindir "github.com/local/interactive-ai-story/backend/internal/domain/director"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/directorrepo"
)

type DirectorApplication interface {
	View(context.Context, timeline.ID) (directorrepo.View, error)
	ApplyExact(context.Context, domaindir.ExactCommand) (domaindir.AuditEntry, error)
	AddInstruction(context.Context, domaindir.InstructionCommand) (narrative.DirectorInstruction, error)
	History(context.Context, timeline.ID, int) ([]domaindir.AuditEntry, error)
	Assist(context.Context, appdir.AssistCommand) (appdir.AssistResult, error)
}

var _ appdir.Service

func registerDirectorRoutes(r chi.Router, a DirectorApplication) {
	r.Get("/api/v1/timelines/{timelineId}/director", func(w http.ResponseWriter, req *http.Request) {
		tid, e := parseTimelineID(chi.URLParam(req, "timelineId"))
		if e != nil {
			http.Error(w, "bad id", 400)
			return
		}
		v, e := a.View(req.Context(), tid)
		if e != nil {
			http.Error(w, e.Error(), 404)
			return
		}
		writeJSON(w, 200, v)
	})
	r.Post("/api/v1/timelines/{timelineId}/director/exact", func(w http.ResponseWriter, req *http.Request) {
		tid, e := parseTimelineID(chi.URLParam(req, "timelineId"))
		if e != nil {
			http.Error(w, "bad id", 400)
			return
		}
		var c domaindir.ExactCommand
		if json.NewDecoder(req.Body).Decode(&c) != nil {
			http.Error(w, "invalid json", 400)
			return
		}
		c.TimelineID = tid
		v, e := a.ApplyExact(req.Context(), c)
		if e != nil {
			code := 422
			if e == domaindir.ErrRevisionMoved {
				code = 409
			}
			http.Error(w, e.Error(), code)
			return
		}
		writeJSON(w, 200, v)
	})
	r.Post("/api/v1/timelines/{timelineId}/director/instructions", func(w http.ResponseWriter, req *http.Request) {
		tid, e := parseTimelineID(chi.URLParam(req, "timelineId"))
		if e != nil {
			http.Error(w, "bad id", 400)
			return
		}
		var c domaindir.InstructionCommand
		if json.NewDecoder(req.Body).Decode(&c) != nil {
			http.Error(w, "invalid json", 400)
			return
		}
		c.TimelineID = tid
		v, e := a.AddInstruction(req.Context(), c)
		if e != nil {
			http.Error(w, e.Error(), 422)
			return
		}
		writeJSON(w, 201, v)
	})
	r.Post("/api/v1/timelines/{timelineId}/director/assist", func(w http.ResponseWriter, req *http.Request) {
		tid, e := parseTimelineID(chi.URLParam(req, "timelineId"))
		if e != nil {
			http.Error(w, "bad id", 400)
			return
		}
		var c appdir.AssistCommand
		if json.NewDecoder(req.Body).Decode(&c) != nil {
			http.Error(w, "invalid json", 400)
			return
		}
		c.TimelineID = tid
		v, e := a.Assist(req.Context(), c)
		if e != nil {
			http.Error(w, e.Error(), 422)
			return
		}
		writeJSON(w, 200, v)
	})
	r.Get("/api/v1/timelines/{timelineId}/director/history", func(w http.ResponseWriter, req *http.Request) {
		tid, e := parseTimelineID(chi.URLParam(req, "timelineId"))
		if e != nil {
			http.Error(w, "bad id", 400)
			return
		}
		limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
		v, e := a.History(req.Context(), tid, limit)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		writeJSON(w, 200, map[string]any{"entries": v})
	})
}
