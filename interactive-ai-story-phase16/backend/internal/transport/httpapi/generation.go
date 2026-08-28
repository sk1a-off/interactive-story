package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type GenerationApplication interface {
	Submit(context.Context, string, appgen.PlayerAction) (id.ID, error)
	Subscribe(id.ID) (<-chan appgen.Update, func())
	Cancel(context.Context, id.ID) error
	Replay(context.Context, id.ID, int64) ([]appgen.Update, error)
}

func registerGenerationRoutes(r chi.Router, app GenerationApplication) {
	r.Post("/api/v1/timelines/{timelineId}/actions", func(w http.ResponseWriter, req *http.Request) {
		tid, err := id.Parse(chi.URLParam(req, "timelineId"))
		if err != nil {
			http.Error(w, "invalid timeline id", 400)
			return
		}
		var body struct {
			ExpectedHead int64  `json:"expectedHead"`
			Text         string `json:"text"`
		}
		if err = json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", 400)
			return
		}
		gid, err := app.Submit(req.Context(), req.Header.Get("Idempotency-Key"), appgen.PlayerAction{TimelineID: timeline.ID(tid), ExpectedHead: body.ExpectedHead, Text: body.Text})
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"generationId": gid})
	})
	r.Get("/api/v1/generations/{generationId}/events", func(w http.ResponseWriter, req *http.Request) {
		gid, err := id.Parse(chi.URLParam(req, "generationId"))
		if err != nil {
			http.Error(w, "invalid generation id", 400)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		ch, cancel := app.Subscribe(gid)
		defer cancel()
		poll := time.NewTicker(time.Second)
		defer poll.Stop()
		after, _ := strconv.ParseInt(req.URL.Query().Get("after"), 10, 64)
		if after == 0 {
			after, _ = strconv.ParseInt(req.Header.Get("Last-Event-ID"), 10, 64)
		}
		last := after
		history, err := app.Replay(req.Context(), gid, after)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, u := range history {
			if u.Sequence <= last {
				continue
			}
			last = u.Sequence
			raw, _ := json.Marshal(u)
			fmt.Fprintf(w, "event: generation\nid: %d\ndata: %s\n\n", u.Sequence, raw)
			flusher.Flush()
			if u.Phase == appgen.PhaseCompleted || u.Phase == appgen.PhaseFailed {
				return
			}
		}
		for {
			select {
			case <-req.Context().Done():
				return
			case <-poll.C:
				// Queue-level terminal transitions (duplicate/stale/cancelled jobs)
				// are persisted without passing through the in-memory hub. Poll the
				// durable stream so an already connected browser still receives them.
				history, replayErr := app.Replay(req.Context(), gid, last)
				if replayErr != nil {
					return
				}
				for _, u := range history {
					if u.Sequence <= last {
						continue
					}
					last = u.Sequence
					raw, _ := json.Marshal(u)
					fmt.Fprintf(w, "event: generation\nid: %d\ndata: %s\n\n", u.Sequence, raw)
					flusher.Flush()
					if u.Phase == appgen.PhaseCompleted || u.Phase == appgen.PhaseFailed {
						return
					}
				}
			case u, ok := <-ch:
				if !ok {
					return
				}
				if u.Sequence > 0 && u.Sequence <= last {
					continue
				}
				if u.Sequence > last {
					last = u.Sequence
				}
				raw, _ := json.Marshal(u)
				fmt.Fprintf(w, "event: generation\nid: %d\ndata: %s\n\n", u.Sequence, raw)
				flusher.Flush()
				if u.Phase == appgen.PhaseCompleted || u.Phase == appgen.PhaseFailed {
					return
				}
			}
		}
	})
	r.Post("/api/v1/generations/{generationId}/cancel", func(w http.ResponseWriter, req *http.Request) {
		gid, err := id.Parse(chi.URLParam(req, "generationId"))
		if err != nil {
			http.Error(w, "invalid generation id", http.StatusBadRequest)
			return
		}
		if err = app.Cancel(req.Context(), gid); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})

}
