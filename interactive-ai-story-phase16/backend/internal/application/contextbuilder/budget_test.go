package contextbuilder

import (
	"github.com/local/interactive-ai-story/backend/internal/ports/memory"
	"testing"
)

func TestBudgetNeverDropsAuthoritativeState(t *testing.T) {
	in := Result{Authoritative: []byte("authoritative-current-state"), RecentBeats: []string{"recent"}, Summaries: []string{"summary"}, Retrieved: []memory.Memory{{Content: "retrieved"}}}
	out := ApplyBudget(in, 1)
	if string(out.Authoritative) != "authoritative-current-state" {
		t.Fatal("authoritative state dropped")
	}
	if len(out.RecentBeats)+len(out.Summaries)+len(out.Retrieved) != 0 {
		t.Fatal("lower priority content survived exhausted budget")
	}
}
