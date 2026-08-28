package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	app "github.com/local/interactive-ai-story/backend/internal/application/imagegeneration"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/imagegeneration"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/imagegenerationrepo"
)

const maxWorkerResultBody int64 = 40 << 20

type ImageApplication interface {
	CreateForScene(context.Context, app.CreateCommand) (domain.View, error)
	CreateNextForScene(context.Context, id.ID, id.ID) (domain.View, error)
	CreateForParagraph(context.Context, app.ParagraphCommand) (domain.View, error)
	Get(context.Context, id.ID) (domain.View, error)
	ClaimNext(context.Context, string) (domain.Generation, error)
	Complete(context.Context, id.ID, string, app.WorkerResult) (domain.View, error)
	Fail(context.Context, id.ID, string, app.WorkerFailure) (domain.Generation, error)
	Heartbeat(context.Context, string, *id.ID) error
	WorkerStatus(context.Context) (repo.WorkerStatus, error)
	SelectImage(context.Context, id.ID, id.ID) error
}

type publicGeneration struct {
	ID              id.ID          `json:"id"`
	SceneID         id.ID          `json:"sceneId"`
	Status          domain.Status  `json:"status"`
	Attempts        int            `json:"attempts"`
	ErrorCode       string         `json:"errorCode,omitempty"`
	Images          []domain.Image `json:"images"`
	SelectedImageID *id.ID         `json:"selectedImageId,omitempty"`
}

func publicView(v domain.View) publicGeneration {
	return publicGeneration{ID: v.Generation.ID, SceneID: v.Generation.SceneID, Status: v.Generation.Status, Attempts: v.Generation.Attempts, ErrorCode: v.Generation.ErrorCode, Images: v.Images, SelectedImageID: v.SelectedImageID}
}

func registerImageRoutes(r chi.Router, a ImageApplication) {
	r.Post("/api/v1/stories/{storyId}/scenes/{sceneId}/images", func(w http.ResponseWriter, req *http.Request) {
		sid, e := id.Parse(chi.URLParam(req, "storyId"))
		if e != nil {
			writeError(w, 400, "invalid_story_id", "invalid story id")
			return
		}
		sceneID, e := id.Parse(chi.URLParam(req, "sceneId"))
		if e != nil {
			writeError(w, 400, "invalid_scene_id", "invalid scene id")
			return
		}
		v, e := a.CreateNextForScene(req.Context(), sid, sceneID)
		if e != nil {
			if errors.Is(e, app.ErrUnsupportedProvider) {
				writeError(w, http.StatusConflict, "IMAGE_PROVIDER_NOT_READY", "Active image provider is not the local Perchance browser worker. Select perchance_browser in AI settings.")
				return
			}
			if errors.Is(e, app.ErrNoIllustrationCandidate) {
				writeError(w, 422, "NO_ILLUSTRATION_CANDIDATE", "В этой сцене больше нет новых абзацев, которым нужна иллюстрация.")
				return
			}
			writeError(w, 422, "IMAGE_GENERATION_FAILED", "Не удалось подобрать новый момент для иллюстрации.")
			return
		}
		writeJSON(w, http.StatusAccepted, publicView(v))
	})
	r.Post("/api/v1/stories/{storyId}/scenes/{sceneId}/images/paragraph", func(w http.ResponseWriter, req *http.Request) {
		sid, e := id.Parse(chi.URLParam(req, "storyId"))
		if e != nil {
			writeError(w, 400, "invalid_story_id", "invalid story id")
			return
		}
		sceneID, e := id.Parse(chi.URLParam(req, "sceneId"))
		if e != nil {
			writeError(w, 400, "invalid_scene_id", "invalid scene id")
			return
		}
		var body struct {
			BeatID    string `json:"beatId"`
			Paragraph int    `json:"paragraph"`
		}
		if json.NewDecoder(req.Body).Decode(&body) != nil {
			writeError(w, 400, "invalid_json", "invalid json")
			return
		}
		beatID, e := id.Parse(body.BeatID)
		if e != nil {
			writeError(w, 422, "invalid_beat_id", "invalid beat id")
			return
		}
		v, e := a.CreateForParagraph(req.Context(), app.ParagraphCommand{StoryID: sid, SceneID: sceneID, BeatID: beatID, Paragraph: body.Paragraph})
		if e != nil {
			switch {
			case errors.Is(e, app.ErrIllustrationExists):
				writeError(w, 409, "ILLUSTRATION_ALREADY_EXISTS", "У этого абзаца уже есть или создаётся иллюстрация.")
			case errors.Is(e, app.ErrParagraphNotIllustratable):
				writeError(w, 422, "PARAGRAPH_NOT_VISUAL", "Этот абзац в основном состоит из реплики или не содержит самостоятельного визуального момента.")
			case errors.Is(e, app.ErrParagraphOutOfRange):
				writeError(w, 422, "PARAGRAPH_NOT_FOUND", "Выбранный абзац больше не существует в актуальном Canon.")
			case errors.Is(e, app.ErrUnsupportedProvider):
				writeError(w, 409, "IMAGE_PROVIDER_NOT_READY", "Active image provider is not the local Perchance browser worker. Select perchance_browser in AI settings.")
			default:
				writeError(w, 422, "IMAGE_GENERATION_FAILED", "Не удалось создать иллюстрацию для этого абзаца.")
			}
			return
		}
		writeJSON(w, http.StatusAccepted, publicView(v))
	})
	r.Get("/api/v1/image-generations/{generationId}", func(w http.ResponseWriter, req *http.Request) {
		gid, e := id.Parse(chi.URLParam(req, "generationId"))
		if e != nil {
			writeError(w, 400, "invalid_generation_id", "invalid generation id")
			return
		}
		v, e := a.Get(req.Context(), gid)
		if e != nil {
			writeError(w, 404, "image_generation_not_found", "Image generation not found.")
			return
		}
		writeJSON(w, 200, publicView(v))
	})
	r.Put("/api/v1/scenes/{sceneId}/selected-image", func(w http.ResponseWriter, req *http.Request) {
		sceneID, e := id.Parse(chi.URLParam(req, "sceneId"))
		if e != nil {
			writeError(w, 400, "invalid_scene_id", "invalid scene id")
			return
		}
		var b struct {
			ImageID string `json:"imageId"`
		}
		if json.NewDecoder(req.Body).Decode(&b) != nil {
			writeError(w, 400, "invalid_json", "invalid json")
			return
		}
		imageID, e := id.Parse(b.ImageID)
		if e != nil {
			writeError(w, 422, "invalid_image_id", "invalid image id")
			return
		}
		if e = a.SelectImage(req.Context(), sceneID, imageID); e != nil {
			writeError(w, 422, "image_selection_failed", "Could not select this image.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	r.Get("/internal/image-worker/jobs/next", func(w http.ResponseWriter, req *http.Request) {
		worker := workerID(req)
		if worker == "" {
			writeError(w, 400, "worker_id_required", "worker id required")
			return
		}
		g, e := a.ClaimNext(req.Context(), worker)
		if errors.Is(e, repo.ErrNoJob) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if e != nil {
			slog.Error("image worker claim failed", "worker_id", worker, "error", e)
			writeError(w, 500, "worker_claim_failed", "could not claim image job")
			return
		}
		writeJSON(w, 200, map[string]any{
			"id": g.ID, "storyId": g.StoryID, "sceneId": g.SceneID, "prompt": g.Prompt, "negative": g.NegativePrompt,
			"style": g.Style, "stylePrompt": g.StylePrompt, "styleNegativePrompt": g.StyleNegativePrompt, "resolution": "768x768", "width": g.Width, "height": g.Height, "count": g.ImageCount, "attempt": g.Attempts,
		})
	})
	r.Post("/internal/image-worker/jobs/{generationId}/result", func(w http.ResponseWriter, req *http.Request) {
		worker := workerID(req)
		if worker == "" {
			writeError(w, 400, "worker_id_required", "worker id required")
			return
		}
		gid, e := id.Parse(chi.URLParam(req, "generationId"))
		if e != nil {
			writeError(w, 400, "invalid_generation_id", "invalid generation id")
			return
		}
		req.Body = http.MaxBytesReader(w, req.Body, maxWorkerResultBody)
		var b struct {
			Images []string `json:"images"`
		}
		dec := json.NewDecoder(req.Body)
		if e = dec.Decode(&b); e != nil {
			writeError(w, 413, "invalid_worker_result", "worker result is invalid or too large")
			return
		}
		v, e := a.Complete(req.Context(), gid, worker, app.WorkerResult{Images: b.Images})
		if e != nil {
			writeError(w, 422, "invalid_worker_result", "worker result rejected")
			return
		}
		writeJSON(w, 200, publicView(v))
	})
	r.Post("/internal/image-worker/jobs/{generationId}/failed", func(w http.ResponseWriter, req *http.Request) {
		worker := workerID(req)
		if worker == "" {
			writeError(w, 400, "worker_id_required", "worker id required")
			return
		}
		gid, e := id.Parse(chi.URLParam(req, "generationId"))
		if e != nil {
			writeError(w, 400, "invalid_generation_id", "invalid generation id")
			return
		}
		var b struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(req.Body).Decode(&b)
		detail := strings.TrimSpace(b.Error)
		if len(detail) > 1000 {
			detail = detail[:1000]
		}
		g, e := a.Fail(req.Context(), gid, worker, app.WorkerFailure{ErrorCode: "provider_failed", Detail: detail})
		if e != nil {
			writeError(w, 409, "worker_failure_rejected", "failure result rejected")
			return
		}
		writeJSON(w, 200, map[string]any{"id": g.ID, "status": g.Status, "attempts": g.Attempts})
	})
	r.Post("/internal/image-worker/heartbeat", func(w http.ResponseWriter, req *http.Request) {
		var b struct {
			WorkerID           string  `json:"workerId"`
			ActiveGenerationID *string `json:"activeGenerationId"`
		}
		if json.NewDecoder(req.Body).Decode(&b) != nil || strings.TrimSpace(b.WorkerID) == "" {
			writeError(w, 400, "invalid_heartbeat", "invalid heartbeat")
			return
		}
		var active *id.ID
		if b.ActiveGenerationID != nil && *b.ActiveGenerationID != "" {
			x, e := id.Parse(*b.ActiveGenerationID)
			if e != nil {
				writeError(w, 422, "invalid_generation_id", "invalid generation id")
				return
			}
			active = &x
		}
		if e := a.Heartbeat(req.Context(), b.WorkerID, active); e != nil {
			writeError(w, 500, "heartbeat_failed", "heartbeat failed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r.Get("/internal/image-worker/status", func(w http.ResponseWriter, req *http.Request) {
		v, e := a.WorkerStatus(req.Context())
		if e != nil {
			writeError(w, 500, "worker_status_failed", "worker status unavailable")
			return
		}
		writeJSON(w, 200, v)
	})
}
func workerID(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Image-Worker-ID")); v != "" {
		return v
	}
	return strings.TrimSpace(r.URL.Query().Get("worker_id"))
}
