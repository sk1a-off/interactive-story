package llmpolicy

import (
	"testing"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func TestSupportsParallelRequestsOnlyForHostedProvider(t *testing.T) {
	if !SupportsParallelRequests(aiport.ProviderIdentity{Provider: "google_gemini"}) {
		t.Fatal("Google Gemini should allow independent concurrent requests")
	}
	if SupportsParallelRequests(aiport.ProviderIdentity{Provider: "openai_compatible", Profile: "llamacpp-16k-kvq8"}) {
		t.Fatal("local OpenAI-compatible model must remain sequential")
	}
}
