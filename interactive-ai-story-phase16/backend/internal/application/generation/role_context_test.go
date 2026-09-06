package generation

import (
	"encoding/json"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

func TestContextWithoutRemovesOnlyExplicitDuplicates(t *testing.T) {
	target := generationtarget.Target{
		StoryBible:  json.RawMessage(`{"name":"world"}`),
		Objectives:  []generationtarget.Objective{{ID: "objective-1", Title: "Goal"}},
		Journal:     []generationtarget.JournalEntry{{ID: "journal-1", Name: "Key"}},
		RecentBeats: []generationtarget.RecentBeat{{Text: "Old fact"}},
	}
	got := contextWithout(target, "objectives", "heroJournal")
	if _, exists := got["objectives"]; exists {
		t.Fatal("objectives duplicate remained in context")
	}
	if _, exists := got["heroJournal"]; exists {
		t.Fatal("journal duplicate remained in context")
	}
	if got["storyBible"] == nil || got["recentBeats"] == nil {
		t.Fatalf("non-duplicate context was lost: %#v", got)
	}
}
