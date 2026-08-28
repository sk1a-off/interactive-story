package timeline

import (
	"errors"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
)

func TestExpectedHead(t *testing.T) {
	tid, _ := id.New()
	sid, _ := id.New()
	tl, err := New(ID(tid), story.ID(sid), "Main")
	if err != nil {
		t.Fatal(err)
	}
	tl.HeadEventSeq = 4
	if err := tl.CheckExpectedHead(4); err != nil {
		t.Fatalf("matching head rejected: %v", err)
	}
	err = tl.CheckExpectedHead(3)
	var conflict HeadConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected HeadConflictError, got %v", err)
	}
	if conflict.Expected != 3 || conflict.Actual != 4 {
		t.Fatalf("unexpected conflict: %+v", conflict)
	}
}
