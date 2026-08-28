package llmpolicy

import aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"

// SupportsParallelRequests is deliberately conservative. The local
// OpenAI-compatible endpoint normally represents one llama.cpp GPU worker, so
// parallel completions would compete for the same accelerator and often make
// latency and KV-cache pressure worse. Google hosts independent request
// execution and benefits from a dependency-aware fan-out.
func SupportsParallelRequests(identity aiport.ProviderIdentity) bool {
	return identity.Provider == "google_gemini"
}
