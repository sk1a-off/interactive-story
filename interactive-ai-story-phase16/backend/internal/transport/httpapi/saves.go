package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/local/interactive-ai-story/backend/internal/application/saves"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/savepoint"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type SaveApplication interface {
	Create(context.Context, saves.CreateCommand) (savepoint.SavePoint, error)
	Preview(context.Context, savepoint.ID) (saves.Preview, error)
	Fork(context.Context, saves.ForkCommand) (timeline.Timeline, error)
	ListTimeline(context.Context, story.ID) ([]timeline.Timeline, error)
	ListSaves(context.Context, timeline.ID) ([]savepoint.SavePoint, error)
	Library(context.Context, story.ID) (saves.Library, error)
	UpdatePresentation(context.Context, saves.PresentationCommand) error
}

type createSaveRequest struct {
	Name   string `json:"name"`
	Note   string `json:"note"`
	Pinned bool   `json:"pinned"`
}

type forkRequest struct {
	SavePointID string `json:"save_point_id"`
	Name        string `json:"name"`
}

type restoreRequest struct {
	Mode            string `json:"mode"`
	NewTimelineName string `json:"newTimelineName"`
}

type timelineDTO struct {
	ID               string  `json:"id"`
	StoryID          string  `json:"storyId"`
	Name             string  `json:"name"`
	Status           string  `json:"status"`
	ParentTimelineID *string `json:"parentTimelineId"`
	ForkedFromSaveID *string `json:"forkedFromSaveId"`
	HeadEventSeq     int64   `json:"headEventSeq"`
	HeadSnapshotID   *string `json:"headSnapshotId"`
}

type savePointDTO struct {
	ID               string  `json:"id"`
	TimelineID       string  `json:"timelineId"`
	Name             string  `json:"name"`
	Note             string  `json:"note"`
	Kind             string  `json:"kind"`
	Pinned           bool    `json:"pinned"`
	EventSeq         int64   `json:"eventSeq"`
	ChapterID        *string `json:"chapterId"`
	SceneID          *string `json:"sceneId"`
	BeatID           *string `json:"beatId"`
	CreatedAt        string  `json:"createdAt"`
	DisplaySlot      *int    `json:"displaySlot,omitempty"`
	ThumbnailStatus  string  `json:"thumbnailStatus"`
	ThumbnailAssetID *string `json:"thumbnailAssetId,omitempty"`
}

type previewDTO struct {
	Save      savePointDTO `json:"save"`
	EventSeq  int64        `json:"eventSeq"`
	StateHash string       `json:"stateHash"`
	State     any          `json:"state"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func registerSaveRoutes(r chi.Router, app SaveApplication) {
	r.Get("/api/v1/stories/{storyId}/timelines", listTimelinesHandler(app))
	r.Get("/api/v1/stories/{storyId}/save-library", saveLibraryHandler(app))
	r.Get("/api/v1/timelines/{timelineId}/saves", listSavesHandler(app))
	r.Post("/api/v1/timelines/{timelineId}/saves", createSaveHandler(app))
	r.Post("/api/v1/timelines/{timelineId}/fork", forkHandler(app))
	r.Post("/api/v1/saves/{saveId}/restore", restoreHandler(app))
	r.Patch("/api/v1/saves/{saveId}", updateSavePresentationHandler(app))
}

func listTimelinesHandler(app SaveApplication) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := parseIDParam(r, "storyId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_story_id", err.Error())
			return
		}
		values, err := app.ListTimeline(r.Context(), story.ID(raw))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "timeline_list_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, timelineDTOs(values))
	}
}

func listSavesHandler(app SaveApplication) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := parseIDParam(r, "timelineId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_timeline_id", err.Error())
			return
		}
		values, err := app.ListSaves(r.Context(), timeline.ID(raw))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "save_list_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, savePointDTOs(values))
	}
}

func createSaveHandler(app SaveApplication) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tid, err := parseIDParam(r, "timelineId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_timeline_id", err.Error())
			return
		}
		var req createSaveRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		created, err := app.Create(r.Context(), saves.CreateCommand{TimelineID: timeline.ID(tid), Name: req.Name, Note: req.Note, Kind: savepoint.KindManual, Pinned: req.Pinned})
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "save_create_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, toSavePointDTO(created))
	}
}

func forkHandler(app SaveApplication) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sourceID, err := parseIDParam(r, "timelineId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_timeline_id", err.Error())
			return
		}
		var req forkRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		saveRaw, err := id.Parse(strings.TrimSpace(req.SavePointID))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_save_id", "save_point_id must be UUID")
			return
		}
		childRaw, err := id.New()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "id_generation_failed", err.Error())
			return
		}
		child, err := app.Fork(r.Context(), saves.ForkCommand{SaveID: savepoint.ID(saveRaw), SourceTimelineID: timeline.ID(sourceID), TimelineID: timeline.ID(childRaw), Name: req.Name})
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "fork_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, toTimelineDTO(child))
	}
}

func restoreHandler(app SaveApplication) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		saveRaw, err := parseIDParam(r, "saveId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_save_id", err.Error())
			return
		}
		var req restoreRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		switch strings.ToLower(strings.TrimSpace(req.Mode)) {
		case "preview":
			preview, err := app.Preview(r.Context(), savepoint.ID(saveRaw))
			if err != nil {
				writeError(w, http.StatusUnprocessableEntity, "preview_failed", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, previewDTO{Save: toSavePointDTO(preview.Save), EventSeq: preview.Snapshot.EventSeq, StateHash: preview.Snapshot.StateHash, State: preview.Snapshot.State})
		case "fork":
			childRaw, err := id.New()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "id_generation_failed", err.Error())
				return
			}
			child, err := app.Fork(r.Context(), saves.ForkCommand{SaveID: savepoint.ID(saveRaw), TimelineID: timeline.ID(childRaw), Name: req.NewTimelineName})
			if err != nil {
				writeError(w, http.StatusUnprocessableEntity, "fork_failed", err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, toTimelineDTO(child))
		case "restore":
			writeError(w, http.StatusConflict, "advanced_restore_required", saves.ErrAdvancedRestoreRequired.Error())
		default:
			writeError(w, http.StatusBadRequest, "invalid_restore_mode", "mode must be fork, preview, or restore")
		}
	}
}

func parseIDParam(r *http.Request, name string) (id.ID, error) {
	return id.Parse(chi.URLParam(r, name))
}
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{Code: code, Message: message})
}

func toTimelineDTO(v timeline.Timeline) timelineDTO {
	out := timelineDTO{ID: id.ID(v.ID).String(), StoryID: id.ID(v.StoryID).String(), Name: v.Name, Status: string(v.Status), HeadEventSeq: v.HeadEventSeq}
	if v.ParentTimelineID != nil {
		x := id.ID(*v.ParentTimelineID).String()
		out.ParentTimelineID = &x
	}
	if v.ForkedFromSaveID != nil {
		x := id.ID(*v.ForkedFromSaveID).String()
		out.ForkedFromSaveID = &x
	}
	if v.HeadSnapshotID != nil {
		x := id.ID(*v.HeadSnapshotID).String()
		out.HeadSnapshotID = &x
	}
	return out
}
func timelineDTOs(values []timeline.Timeline) []timelineDTO {
	out := make([]timelineDTO, 0, len(values))
	for _, v := range values {
		out = append(out, toTimelineDTO(v))
	}
	return out
}
func ptrID(v *id.ID) *string {
	if v == nil {
		return nil
	}
	x := v.String()
	return &x
}
func toSavePointDTO(v savepoint.SavePoint) savePointDTO {
	return savePointDTO{ID: id.ID(v.ID).String(), TimelineID: id.ID(v.TimelineID).String(), Name: v.Name, Note: v.Note, Kind: string(v.Kind), Pinned: v.Pinned, EventSeq: v.EventSeq, ChapterID: ptrID(v.ChapterID), SceneID: ptrID(v.SceneID), BeatID: ptrID(v.BeatID), CreatedAt: v.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z"), DisplaySlot: v.DisplaySlot, ThumbnailStatus: v.ThumbnailStatus, ThumbnailAssetID: ptrID(v.ThumbnailAssetID)}
}
func savePointDTOs(values []savepoint.SavePoint) []savePointDTO {
	out := make([]savePointDTO, 0, len(values))
	for _, v := range values {
		out = append(out, toSavePointDTO(v))
	}
	return out
}

func saveLibraryHandler(app SaveApplication) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, e := parseIDParam(r, "storyId")
		if e != nil {
			writeError(w, 400, "invalid_story_id", e.Error())
			return
		}
		lib, e := app.Library(r.Context(), story.ID(raw))
		if e != nil {
			writeError(w, 500, "save_library_failed", e.Error())
			return
		}
		savesByTimeline := map[string][]savePointDTO{}
		for tid, values := range lib.Saves {
			savesByTimeline[id.ID(tid).String()] = savePointDTOs(values)
		}
		writeJSON(w, 200, map[string]any{"timelines": timelineDTOs(lib.Timelines), "saves": savesByTimeline})
	}
}
func updateSavePresentationHandler(app SaveApplication) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, e := parseIDParam(r, "saveId")
		if e != nil {
			writeError(w, 400, "invalid_save_id", e.Error())
			return
		}
		var req struct {
			DisplaySlot *int `json:"displaySlot"`
			Pinned      bool `json:"pinned"`
		}
		if e = decodeJSON(r, &req); e != nil {
			writeError(w, 400, "invalid_json", e.Error())
			return
		}
		if e = app.UpdatePresentation(r.Context(), saves.PresentationCommand{SaveID: savepoint.ID(raw), DisplaySlot: req.DisplaySlot, Pinned: req.Pinned}); e != nil {
			writeError(w, 422, "save_update_failed", e.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
