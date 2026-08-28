package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	app "github.com/local/interactive-ai-story/backend/internal/application/storysetup"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/setup"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/readerrepo"
	"net/http"
	"strings"
)

type StorySetupApplication interface {
	List(context.Context) ([]app.StoryListItem, error)
	Delete(context.Context, story.ID) error
	Create(context.Context, app.CreateCommand) (story.Story, error)
	Generate(context.Context, app.GenerateCommand) ([]setup.Component, error)
	Assist(context.Context, app.AssistCommand) (app.AssistResult, error)
	Edit(context.Context, app.ManualEditCommand) (setup.Component, error)
	SetLock(context.Context, story.ID, setup.ComponentKey, bool) (setup.Component, error)
	Start(context.Context, story.ID) (app.StartResult, error)
}
type SetupReader interface {
	ListComponents(context.Context, story.ID) ([]setup.Component, error)
}
type CurrentReader interface {
	Current(context.Context, timeline.ID) (readerrepo.Current, error)
}

func registerStorySetupRoutes(r chi.Router, a StorySetupApplication, repo SetupReader, reader CurrentReader) {
	r.Get("/api/v1/stories", func(w http.ResponseWriter, req *http.Request) {
		items, e := a.List(req.Context())
		if e != nil {
			http.Error(w, e.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"stories": items})
	})
	r.Delete("/api/v1/stories/{storyId}", func(w http.ResponseWriter, req *http.Request) {
		sid, e := parseStoryID(chi.URLParam(req, "storyId"))
		if e != nil {
			http.Error(w, "invalid story id", http.StatusBadRequest)
			return
		}
		if e = a.Delete(req.Context(), sid); e != nil {
			if errors.Is(e, app.ErrStoryNotFound) {
				http.Error(w, e.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, e.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Post("/api/v1/stories", func(w http.ResponseWriter, req *http.Request) {
		var b struct {
			Title, Idea string
			Tags        []string
			Config      map[string]any
		}
		if json.NewDecoder(req.Body).Decode(&b) != nil {
			http.Error(w, "invalid json", 400)
			return
		}
		v, e := a.Create(req.Context(), app.CreateCommand{Title: b.Title, Idea: b.Idea, Tags: b.Tags})
		if e != nil {
			http.Error(w, e.Error(), 422)
			return
		}
		writeJSON(w, 201, v)
	})
	r.Get("/api/v1/stories/{storyId}/setup", func(w http.ResponseWriter, req *http.Request) {
		sid, e := parseStoryID(chi.URLParam(req, "storyId"))
		if e != nil {
			http.Error(w, "invalid story id", 400)
			return
		}
		v, e := repo.ListComponents(req.Context(), sid)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		writeJSON(w, 200, map[string]any{"components": v})
	})
	r.Post("/api/v1/stories/{storyId}/setup/generate", generateSetup(a, false))
	r.Post("/api/v1/stories/{storyId}/setup/regenerate", generateSetup(a, true))
	r.Post("/api/v1/stories/{storyId}/setup/assist", func(w http.ResponseWriter, req *http.Request) {
		sid, e := parseStoryID(chi.URLParam(req, "storyId"))
		if e != nil {
			http.Error(w, "bad id", 400)
			return
		}
		var b struct {
			Instruction string                     `json:"instruction"`
			Components  []string                   `json:"components"`
			Drafts      map[string]json.RawMessage `json:"drafts"`
			Action      string                     `json:"action"`
			Target      app.AssistTarget           `json:"target"`
		}
		if json.NewDecoder(req.Body).Decode(&b) != nil {
			http.Error(w, "invalid json", 400)
			return
		}
		keys := make([]setup.ComponentKey, 0, len(b.Components))
		for _, raw := range b.Components {
			key, parseErr := setup.ParseKey(strings.TrimSpace(raw))
			if parseErr != nil {
				http.Error(w, "bad component", 422)
				return
			}
			keys = append(keys, key)
		}
		drafts := make(map[setup.ComponentKey]json.RawMessage, len(b.Drafts))
		for rawKey, payload := range b.Drafts {
			key, parseErr := setup.ParseKey(strings.TrimSpace(rawKey))
			if parseErr != nil {
				http.Error(w, "bad draft component", 422)
				return
			}
			drafts[key] = payload
		}
		v, e := a.Assist(req.Context(), app.AssistCommand{StoryID: sid, Keys: keys, Instruction: b.Instruction, Action: b.Action, Target: b.Target, Drafts: drafts})
		if e != nil {
			http.Error(w, e.Error(), 422)
			return
		}
		writeJSON(w, 200, v)
	})
	r.Patch("/api/v1/stories/{storyId}/setup", func(w http.ResponseWriter, req *http.Request) {
		sid, e := parseStoryID(chi.URLParam(req, "storyId"))
		if e != nil {
			http.Error(w, "invalid story id", 400)
			return
		}
		var b struct {
			ComponentKey string          `json:"componentKey"`
			Payload      json.RawMessage `json:"payload"`
			Locked       *bool           `json:"locked"`
		}
		if json.NewDecoder(req.Body).Decode(&b) != nil {
			http.Error(w, "invalid json", 400)
			return
		}
		key, e := setup.ParseKey(b.ComponentKey)
		if e != nil {
			http.Error(w, e.Error(), 422)
			return
		}
		v, e := a.Edit(req.Context(), app.ManualEditCommand{StoryID: sid, Key: key, Payload: b.Payload, Locked: b.Locked})
		if e != nil {
			http.Error(w, e.Error(), 422)
			return
		}
		writeJSON(w, 200, v)
	})
	r.Put("/api/v1/stories/{storyId}/setup/components/{key}/lock", func(w http.ResponseWriter, req *http.Request) {
		sid, e := parseStoryID(chi.URLParam(req, "storyId"))
		if e != nil {
			http.Error(w, "bad id", 400)
			return
		}
		key, e := setup.ParseKey(chi.URLParam(req, "key"))
		if e != nil {
			http.Error(w, "bad key", 400)
			return
		}
		var b struct {
			Locked bool `json:"locked"`
		}
		if json.NewDecoder(req.Body).Decode(&b) != nil {
			http.Error(w, "invalid json", 400)
			return
		}
		v, e := a.SetLock(req.Context(), sid, key, b.Locked)
		if e != nil {
			http.Error(w, e.Error(), 422)
			return
		}
		writeJSON(w, 200, v)
	})
	r.Post("/api/v1/stories/{storyId}/start", func(w http.ResponseWriter, req *http.Request) {
		sid, e := parseStoryID(chi.URLParam(req, "storyId"))
		if e != nil {
			http.Error(w, "bad id", 400)
			return
		}
		v, e := a.Start(req.Context(), sid)
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		writeJSON(w, 201, v)
	})
	r.Get("/api/v1/timelines/{timelineId}/current", func(w http.ResponseWriter, req *http.Request) {
		tid, e := parseTimelineID(chi.URLParam(req, "timelineId"))
		if e != nil {
			http.Error(w, "bad id", 400)
			return
		}
		v, e := reader.Current(req.Context(), tid)
		if e != nil {
			http.Error(w, e.Error(), 404)
			return
		}
		writeJSON(w, 200, v)
	})
}
func generateSetup(a StorySetupApplication, _ bool) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		sid, e := parseStoryID(chi.URLParam(req, "storyId"))
		if e != nil {
			http.Error(w, "bad id", 400)
			return
		}
		var b struct {
			Components []string `json:"components"`
		}
		_ = json.NewDecoder(req.Body).Decode(&b)
		keys := []setup.ComponentKey{}
		for _, x := range b.Components {
			k, e := setup.ParseKey(strings.TrimSpace(x))
			if e != nil {
				http.Error(w, "bad component", 422)
				return
			}
			keys = append(keys, k)
		}
		v, e := a.Generate(req.Context(), app.GenerateCommand{StoryID: sid, Keys: keys})
		if e != nil {
			if errors.Is(e, app.ErrGenerationActive) {
				http.Error(w, e.Error(), http.StatusConflict)
				return
			}
			http.Error(w, e.Error(), 422)
			return
		}
		writeJSON(w, 200, map[string]any{"components": v})
	}
}
func parseStoryID(v string) (story.ID, error)       { x, e := id.Parse(v); return story.ID(x), e }
func parseTimelineID(v string) (timeline.ID, error) { x, e := id.Parse(v); return timeline.ID(x), e }
