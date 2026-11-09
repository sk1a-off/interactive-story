package postgres

import (
	"context"
	"math"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
)

func TestHybridSearchRejectsInvalidInputBeforeDatabaseAccess(t *testing.T) {
	story, _ := id.New()
	tid, _ := id.New()
	valid := memory.Query{StoryID: story, TimelineID: timeline.ID(tid), OwnerType: memory.OwnerTimeline, OwnerID: tid,
		Vector: make([]float32, 384), Profile: memory.EmbeddingProfile{Provider: "test", Model: "384", Revision: 1}}
	valid.Vector[0] = 1
	if err := validateHybridQuery(valid); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*memory.Query){
		"story":           func(q *memory.Query) { q.StoryID = "" },
		"timeline":        func(q *memory.Query) { q.TimelineID = "" },
		"owner":           func(q *memory.Query) { q.OwnerID = story },
		"owner type":      func(q *memory.Query) { q.OwnerType = "unknown" },
		"story owner":     func(q *memory.Query) { q.OwnerType = memory.OwnerStory },
		"negative cursor": func(q *memory.Query) { q.AtEventSeq = -1 },
		"provider":        func(q *memory.Query) { q.Profile.Provider = " " },
		"model":           func(q *memory.Query) { q.Profile.Model = "" },
		"revision":        func(q *memory.Query) { q.Profile.Revision = 0 },
		"dimensions":      func(q *memory.Query) { q.Vector = []float32{1} },
		"zero vector":     func(q *memory.Query) { q.Vector = make([]float32, 384) },
		"nan":             func(q *memory.Query) { q.Vector[0] = float32(math.NaN()) },
		"infinity":        func(q *memory.Query) { q.Vector[0] = float32(math.Inf(1)) },
		"limit":           func(q *memory.Query) { q.Limit = 101 },
		"text":            func(q *memory.Query) { q.Text = string(make([]byte, 16385)) },
	} {
		t.Run(name, func(t *testing.T) {
			q := valid
			q.Vector = append([]float32(nil), valid.Vector...)
			change(&q)
			// A nil database panics on access, so an error proves validation
			// stopped the request before either lineage or ranking queries.
			if _, err := NewHybridMemoryRetriever(nil).Search(context.Background(), q); err == nil {
				t.Fatal("invalid query accepted")
			}
		})
	}
}
