package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type healthResponse struct {
	Status string `json:"status"`
}

type ReadinessChecker interface {
	Ping(context.Context) error
}

const readinessTimeout = 2 * time.Second

func NewRouter() http.Handler {
	return NewRouterWithApplications(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func NewRouterWithSaveApplication(saveApp SaveApplication) http.Handler {
	return NewRouterWithApplications(saveApp, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func NewRouterWithApplications(saveApp SaveApplication, generationApp GenerationApplication, setupApp StorySetupApplication, setupReader SetupReader, currentReader CurrentReader, directorApp DirectorApplication, aiSettings AISettingsApplication, promptSettings PromptSettingsApplication, imageApp ImageApplication, narrationApp NarrationApplication) http.Handler {
	return NewRouterWithApplicationsAndReadiness(saveApp, generationApp, setupApp, setupReader, currentReader, directorApp, aiSettings, promptSettings, imageApp, narrationApp, nil)
}

func NewRouterWithApplicationsAndReadiness(saveApp SaveApplication, generationApp GenerationApplication, setupApp StorySetupApplication, setupReader SetupReader, currentReader CurrentReader, directorApp DirectorApplication, aiSettings AISettingsApplication, promptSettings PromptSettingsApplication, imageApp ImageApplication, narrationApp NarrationApplication, readiness ReadinessChecker) http.Handler {
	r := chi.NewRouter()
	r.Get("/health", readinessHandler(readiness, readinessTimeout))
	r.Get("/health/live", healthHandler("live"))
	r.Get("/health/ready", readinessHandler(readiness, readinessTimeout))
	if saveApp != nil {
		registerSaveRoutes(r, saveApp)
	}
	if generationApp != nil {
		registerGenerationRoutes(r, generationApp)
	}
	if setupApp != nil && setupReader != nil && currentReader != nil {
		registerStorySetupRoutes(r, setupApp, setupReader, currentReader)
	}
	if directorApp != nil {
		registerDirectorRoutes(r, directorApp)
	}
	if aiSettings != nil {
		registerAISettingsRoutes(r, aiSettings)
	}
	if promptSettings != nil {
		registerPromptSettingsRoutes(r, promptSettings)
	}
	if imageApp != nil {
		registerImageRoutes(r, imageApp)
	}
	if narrationApp != nil {
		registerNarrationRoutes(r, narrationApp)
	}
	return r
}

func readinessHandler(checker ReadinessChecker, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		statusCode := http.StatusOK
		status := "ready"
		if checker != nil {
			ctx, cancel := context.WithTimeout(req.Context(), timeout)
			err := checker.Ping(ctx)
			cancel()
			if err != nil {
				statusCode = http.StatusServiceUnavailable
				status = "not_ready"
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(healthResponse{Status: status})
	}
}

func healthHandler(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{Status: status})
	}
}
