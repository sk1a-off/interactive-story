package memory

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

func testID(n int) id.ID             { return id.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012d", n)) }
func testTimeline(n int) timeline.ID { return timeline.ID(testID(n)) }
func timelinePtr(n int) *timeline.ID { value := testTimeline(n); return &value }
func sequencePtr(n int64) *int64     { return &n }

func fixture() []LineageNode {
	return []LineageNode{
		{StoryID: testID(1), TimelineID: testTimeline(10), HeadEventSeq: 200},
		{StoryID: testID(1), TimelineID: testTimeline(11), ParentTimelineID: timelinePtr(10), ForkEventSeq: 100, HeadEventSeq: 160},
		{StoryID: testID(1), TimelineID: testTimeline(12), ParentTimelineID: timelinePtr(11), ForkEventSeq: 130, HeadEventSeq: 150},
		{StoryID: testID(1), TimelineID: testTimeline(13), ParentTimelineID: timelinePtr(10), ForkEventSeq: 100, HeadEventSeq: 170},
		{StoryID: testID(1), TimelineID: testTimeline(14), ParentTimelineID: timelinePtr(10), ForkEventSeq: 60, HeadEventSeq: 60},
	}
}

func TestNestedForkVisibilityClipsEveryAncestor(t *testing.T) {
	plan, err := ResolveVisibility(testID(1), testTimeline(12), 140, fixture())
	if err != nil {
		t.Fatal(err)
	}
	want := []HistorySegment{
		{TimelineID: testTimeline(12), AfterEventSeq: 130, AtEventSeq: 140},
		{TimelineID: testTimeline(11), AfterEventSeq: 100, AtEventSeq: 130},
		{TimelineID: testTimeline(10), AfterEventSeq: 0, AtEventSeq: 100},
	}
	if !reflect.DeepEqual(plan.Segments(), want) {
		t.Fatalf("segments=%+v", plan.Segments())
	}
	for _, check := range []struct {
		timeline, seq int
		allowed       bool
	}{
		{10, 1, true}, {10, 100, true}, {10, 101, false},
		{11, 100, false}, {11, 101, true}, {11, 130, true}, {11, 131, false},
		{12, 130, false}, {12, 131, true}, {12, 140, true}, {12, 141, false},
		{13, 101, false}, {14, 60, false},
	} {
		if got := plan.AllowsHistorical(testID(1), testTimeline(check.timeline), int64(check.seq)); got != check.allowed {
			t.Errorf("timeline=%d seq=%d allowed=%v", check.timeline, check.seq, got)
		}
	}
	if plan.AllowsHistorical(testID(2), testTimeline(10), 50) {
		t.Fatal("foreign story visible")
	}
	copy := plan.Segments()
	copy[0].AtEventSeq = 999
	if plan.AllowsHistorical(testID(1), testTimeline(12), 999) {
		t.Fatal("caller mutated trusted visibility")
	}
}

func TestRestoreExcludesAbandonedFuture(t *testing.T) {
	plan, err := ResolveVisibility(testID(1), testTimeline(14), 60, fixture())
	if err != nil {
		t.Fatal(err)
	}
	if !plan.AllowsHistorical(testID(1), testTimeline(10), 60) || plan.AllowsHistorical(testID(1), testTimeline(10), 61) || plan.AllowsHistorical(testID(1), testTimeline(11), 101) {
		t.Fatal("restore leaked future or sibling")
	}
	if !plan.AllowsBranchState(testID(1), testTimeline(14), 60, nil) {
		t.Fatal("restored snapshot baseline missing")
	}
}

func TestBranchStateRejectsAncestorAndExpiredVersions(t *testing.T) {
	plan, err := ResolveVisibility(testID(1), testTimeline(12), 140, fixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		story, timeline int
		from            int64
		to              *int64
		allowed         bool
	}{
		{1, 12, 130, nil, true}, {1, 12, 140, nil, true},
		{1, 12, 130, sequencePtr(139), false}, {1, 12, 130, sequencePtr(140), true},
		{1, 12, 129, nil, false}, {1, 12, 141, nil, false},
		{1, 10, 50, nil, false}, {1, 11, 100, nil, false},
		{1, 13, 130, nil, false}, {2, 12, 130, nil, false},
		{1, 12, 135, sequencePtr(134), false},
	} {
		if got := plan.AllowsBranchState(testID(check.story), testTimeline(check.timeline), check.from, check.to); got != check.allowed {
			t.Errorf("state %+v allowed=%v", check, got)
		}
	}
}

func TestMalformedLineageFailsClosed(t *testing.T) {
	for _, scenario := range []string{"missing", "foreign", "cycle", "duplicate", "before birth", "future", "root boundary", "negative boundary", "parent save before birth"} {
		t.Run(scenario, func(t *testing.T) {
			nodes, at := fixture(), int64(140)
			switch scenario {
			case "missing":
				nodes = nodes[1:]
			case "foreign":
				nodes[0].StoryID = testID(2)
			case "cycle":
				nodes[0].ParentTimelineID = timelinePtr(12)
			case "duplicate":
				nodes = append(nodes, nodes[0])
			case "before birth":
				at = 129
			case "future":
				at = 151
			case "root boundary":
				nodes[0].ForkEventSeq = 1
			case "negative boundary":
				nodes[1].ForkEventSeq = -1
			case "parent save before birth":
				nodes[1].ForkEventSeq = 131
			}
			plan, err := ResolveVisibility(testID(1), testTimeline(12), at, nodes)
			if !errors.Is(err, ErrInvalidVisibility) || plan.AllowsHistorical(testID(1), testTimeline(10), 1) {
				t.Fatalf("error=%v plan=%+v", err, plan)
			}
		})
	}
}
