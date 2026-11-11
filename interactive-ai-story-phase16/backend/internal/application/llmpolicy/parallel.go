package llmpolicy

import aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"

// Hosted models and browser workers execute independent requests concurrently.
// A local llama.cpp endpoint still represents one GPU worker and stays serial.
func SupportsParallelRequests(identity aiport.ProviderIdentity) bool {
	return identity.Provider == "google_gemini" || identity.Provider == "chatgpt_browser"
}
