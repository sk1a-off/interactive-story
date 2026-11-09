package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	imageapp "github.com/local/interactive-ai-story/backend/internal/application/imagegeneration"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	imagedomain "github.com/local/interactive-ai-story/backend/internal/domain/imagegeneration"
	imagerepo "github.com/local/interactive-ai-story/backend/internal/ports/imagegenerationrepo"
)

func TestDecodeJSONRequestContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		body        string
		contentType string
		limit       int64
		wantStatus  int
	}{
		{name: "valid", body: `{"name":"story"}`, contentType: "application/json; charset=utf-8", wantStatus: http.StatusOK},
		{name: "missing content type", body: `{}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "form content type", body: `{}`, contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusUnsupportedMediaType},
		{name: "malformed", body: `{`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "multiple values", body: `{} {}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "too large", body: `{"name":"story"}`, contentType: "application/json", limit: 4, wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			if test.contentType != "" {
				req.Header.Set("Content-Type", test.contentType)
			}
			res := httptest.NewRecorder()
			var body struct {
				Name string `json:"name"`
			}
			ok := false
			if test.limit > 0 {
				ok = decodeJSONRequestWithLimit(res, req, &body, test.limit)
			} else {
				ok = decodeJSONRequest(res, req, &body)
			}
			if test.wantStatus == http.StatusOK {
				if !ok || body.Name != "story" {
					t.Fatalf("ok=%v body=%+v response=%s", ok, body, res.Body.String())
				}
				return
			}
			if ok || res.Code != test.wantStatus {
				t.Fatalf("ok=%v status=%d body=%s", ok, res.Code, res.Body.String())
			}
		})
	}
}

func TestDecodeStrictJSONRequestRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"known":true,"unknown":true}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	var body struct {
		Known bool `json:"known"`
	}
	if decodeStrictJSONRequest(res, req, &body) || res.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestWriteErrorHidesInternalDetails(t *testing.T) {
	t.Parallel()
	res := httptest.NewRecorder()
	writeError(res, http.StatusInternalServerError, "failed", "secret database detail")
	if strings.Contains(res.Body.String(), "secret") || !strings.Contains(res.Body.String(), http.StatusText(http.StatusInternalServerError)) {
		t.Fatalf("body=%s", res.Body.String())
	}
}

func TestImageWorkerFailureRejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	app := &imageApplicationStub{}
	router := chi.NewRouter()
	registerImageRoutes(router, app)
	generationID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/internal/image-worker/jobs/"+generationID.String()+"/failed", strings.NewReader(`{"error":`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Image-Worker-ID", "worker-1")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest || app.failCalls != 0 {
		t.Fatalf("status=%d failCalls=%d body=%s", res.Code, app.failCalls, res.Body.String())
	}
}

type imageApplicationStub struct {
	failCalls int
	claimed   imagedomain.Generation
}

func (*imageApplicationStub) CreateForScene(context.Context, imageapp.CreateCommand) (imagedomain.View, error) {
	return imagedomain.View{}, errors.New("not implemented")
}
func (*imageApplicationStub) CreateNextForScene(context.Context, id.ID, id.ID) (imagedomain.View, error) {
	return imagedomain.View{}, errors.New("not implemented")
}
func (*imageApplicationStub) CreateForParagraph(context.Context, imageapp.ParagraphCommand) (imagedomain.View, error) {
	return imagedomain.View{}, errors.New("not implemented")
}
func (*imageApplicationStub) Get(context.Context, id.ID) (imagedomain.View, error) {
	return imagedomain.View{}, errors.New("not implemented")
}
func (s *imageApplicationStub) ClaimNext(context.Context, string) (imagedomain.Generation, error) {
	return s.claimed, nil
}
func (*imageApplicationStub) Complete(context.Context, id.ID, string, imageapp.WorkerResult) (imagedomain.View, error) {
	return imagedomain.View{}, errors.New("not implemented")
}
func (s *imageApplicationStub) Fail(context.Context, id.ID, string, imageapp.WorkerFailure) (imagedomain.Generation, error) {
	s.failCalls++
	return imagedomain.Generation{}, nil
}
func (*imageApplicationStub) Heartbeat(context.Context, string, *id.ID) error { return nil }
func (*imageApplicationStub) WorkerStatus(context.Context) (imagerepo.WorkerStatus, error) {
	return imagerepo.WorkerStatus{}, nil
}
func (*imageApplicationStub) SelectImage(context.Context, id.ID, id.ID) error { return nil }

func TestImageWorkerClaimReturnsPinnedOutputParameters(t *testing.T) {
	t.Parallel()
	app := &imageApplicationStub{claimed: imagedomain.Generation{Width: 512, Height: 768, ImageCount: 4}}
	router := chi.NewRouter()
	registerImageRoutes(router, app)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/internal/image-worker/jobs/next?worker_id=test", nil))
	var payload struct {
		Resolution string `json:"resolution"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		Count      int    `json:"count"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusOK || payload.Resolution != "512x768" || payload.Width != 512 || payload.Height != 768 || payload.Count != 4 {
		t.Fatalf("status=%d payload=%+v", res.Code, payload)
	}
}
