package narrative

import (
	"encoding/json"
	"errors"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"testing"
)

func tid(n string) id.ID { return id.MustParse("00000000-0000-4000-8000-" + n) }
func TestFactsKnowledgeAndBeliefsRemainDistinct(t *testing.T) {
	timelineID := tid("000000000001")
	factID := tid("000000000002")
	alice := tid("000000000003")
	s := NewState(timelineID)
	f := Fact{ID: factID, TimelineID: timelineID, SubjectType: "world", SubjectID: tid("000000000004"), Predicate: "door_locked", Object: json.RawMessage(`true`), Status: FactActive, ValidFromSeq: 4}
	if err := s.PutFact(f); err != nil {
		t.Fatal(err)
	}
	k, _ := NewKnowledge(timelineID, factID, alice, 1, 5)
	if err := s.GrantKnowledge(k); err != nil {
		t.Fatal(err)
	}
	b, _ := NewBelief(timelineID, tid("000000000005"), alice, Disbelieves, .8)
	b.SubjectType = "world"
	b.Predicate = "door_locked"
	b.Object = json.RawMessage(`false`)
	if err := s.PutBelief(b); err != nil {
		t.Fatal(err)
	}
	if len(s.Facts) != 1 || len(s.Knowledge) != 1 || len(s.Beliefs) != 1 {
		t.Fatal("semantic stores collapsed")
	}
	if s.Beliefs[b.ID].Stance != Disbelieves {
		t.Fatal("belief overwritten by fact")
	}
}
func TestKnowledgeRequiresActiveFact(t *testing.T) {
	timelineID := tid("000000000011")
	s := NewState(timelineID)
	k, _ := NewKnowledge(timelineID, tid("000000000012"), tid("000000000013"), 1, 1)
	if !errors.Is(s.GrantKnowledge(k), ErrNotFound) {
		t.Fatal("knowledge without fact must fail")
	}
}
func TestRelationshipIsDirectedAndCannotSelfReference(t *testing.T) {
	timelineID := tid("000000000021")
	a := tid("000000000022")
	b := tid("000000000023")
	ab, e := NewRelationship(timelineID, tid("000000000024"), a, b)
	if e != nil {
		t.Fatal(e)
	}
	ba, e := NewRelationship(timelineID, tid("000000000025"), b, a)
	if e != nil {
		t.Fatal(e)
	}
	if ab.FromCharacterID != ba.ToCharacterID || ab.ToCharacterID != ba.FromCharacterID {
		t.Fatal("direction lost")
	}
	if _, e = NewRelationship(timelineID, tid("000000000026"), a, a); !errors.Is(e, ErrSelfRelationship) {
		t.Fatal("self relationship allowed")
	}
}
func TestItemOwnershipIsExclusive(t *testing.T) {
	timelineID := tid("000000000031")
	item := tid("000000000032")
	owner := tid("000000000033")
	loc := tid("000000000034")
	if _, e := NewItemState(timelineID, item, owner, loc); !errors.Is(e, ErrInvalidItemState) {
		t.Fatal("dual ownership allowed")
	}
	s, _ := NewItemState(timelineID, item, owner, "")
	s = s.PlaceAt(loc)
	if !s.OwnerCharacterID.IsZero() || s.LocationID != loc {
		t.Fatal("place failed")
	}
	s = s.TransferTo(owner)
	if s.OwnerCharacterID != owner || !s.LocationID.IsZero() {
		t.Fatal("transfer failed")
	}
}
func TestTimelineIsolation(t *testing.T) {
	a := NewState(tid("000000000041"))
	b := tid("000000000042")
	r, _ := NewRelationship(b, tid("000000000043"), tid("000000000044"), tid("000000000045"))
	if !errors.Is(a.PutRelationship(r), ErrTimelineMismatch) {
		t.Fatal("cross timeline state leaked")
	}
}
func TestCharacterAgeAndConfidenceValidation(t *testing.T) {
	_, e := NewCharacter(tid("000000000051"), tid("000000000052"), CharacterPlayer, "P", 0)
	if !errors.Is(e, ErrInvalidCharacterAge) {
		t.Fatal("age invariant failed")
	}
	_, e = NewKnowledge(tid("000000000053"), tid("000000000054"), tid("000000000055"), 1.1, 1)
	if !errors.Is(e, ErrInvalidConfidence) {
		t.Fatal("confidence invariant failed")
	}
}

func TestCloneForTimelineRebindsAndSeparatesMaps(t *testing.T) {
	sourceID := tid("000000000061")
	childID := tid("000000000062")
	factID := tid("000000000063")
	s := NewState(sourceID)
	if err := s.PutFact(Fact{ID: factID, TimelineID: sourceID, SubjectType: "world", Predicate: "x", Object: json.RawMessage(`true`), Status: FactActive, ValidFromSeq: 1}); err != nil {
		t.Fatal(err)
	}
	child := s.CloneForTimeline(childID)
	if child.TimelineID != childID || child.Facts[factID].TimelineID != childID {
		t.Fatal("clone did not rebind timeline")
	}
	delete(child.Facts, factID)
	if _, ok := s.Facts[factID]; !ok {
		t.Fatal("child mutation aliased parent map")
	}
}
