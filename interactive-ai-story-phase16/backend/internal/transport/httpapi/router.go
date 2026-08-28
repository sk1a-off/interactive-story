package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type healthResponse struct {
	Status string `json:"status"`
}

func NewRouter() http.Handler {
	return NewRouterWithApplications(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func NewRouterWithSaveApplication(saveApp SaveApplication) http.Handler {
	return NewRouterWithApplications(saveApp, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func NewRouterWithApplications(saveApp SaveApplication, generationApp GenerationApplication, setupApp StorySetupApplication, setupReader SetupReader, currentReader CurrentReader, directorApp DirectorApplication, aiSettings AISettingsApplication, promptSettings PromptSettingsApplication, imageApp ImageApplication, narrationApp NarrationApplication) http.Handler {
	r := chi.NewRouter()
	r.Get("/health", healthHandler("ready"))
	r.Get("/health/live", healthHandler("live"))
	r.Get("/health/ready", healthHandler("ready"))
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

func healthHandler(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{Status: status})
	}
}
