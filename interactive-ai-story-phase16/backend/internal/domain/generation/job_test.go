package generation

import (
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/aiconfig"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

func iid(s string) id.ID { return id.MustParse("00000000-0000-4000-8000-" + s) }
func cfg(t *testing.T) aiconfig.Revision {
	c, e := aiconfig.NewRevision(iid("000000000001"), 7,
		aiport.ProviderIdentity{Kind: aiport.KindStoryLLM, Provider: "fake", Model: "story-v1", Profile: "a"},
		aiport.ProviderIdentity{Kind: aiport.KindEmbedding, Provider: "fake", Model: "embed-v1"},
		aiport.ProviderIdentity{Kind: aiport.KindImage, Provider: "fake", Model: "image-v1"})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestJobPinsProviderAtCreation(t *testing.T) {
	c := cfg(t)
	j, e := NewJob(iid("000000000002"), iid("000000000003"), iid("000000000004"), 12, c, aiport.KindStoryLLM, "r")
	if e != nil {
		t.Fatal(e)
	}
	c.StoryLLM.Model = "story-v2"
	if j.Provider.Model != "story-v1" || j.ConfigRevisionID != iid("000000000001") {
		t.Fatal("existing job changed after config switch")
	}
}
func TestLifecycle(t *testing.T) {
	j, e := NewJob(iid("000000000012"), iid("000000000013"), iid("000000000014"), 0, cfg(t), aiport.KindImage, "")
	if e != nil {
		t.Fatal(e)
	}
	if e = j.Start(); e != nil {
		t.Fatal(e)
	}
	if e = j.Complete(); e != nil {
		t.Fatal(e)
	}
	if e = j.Start(); e != ErrInvalidTransition {
		t.Fatal("completed job restarted")
	}
}
