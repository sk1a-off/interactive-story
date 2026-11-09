package memory

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/projection"
)

func TestStateDocumentsKeepTruthKnowledgeAndBeliefSeparate(t *testing.T) {
	state := projection.Empty(testTimeline(1))
	state.LastEventSeq = 20
	factID, actor := testID(2), testID(3)
	state.Narrative.Facts[factID] = narrative.Fact{ID: factID, TimelineID: testID(1), Predicate: "door", Object: json.RawMessage(`"locked"`), Status: narrative.FactActive}
	state.Narrative.Knowledge["fact-actor"] = narrative.Knowledge{TimelineID: testID(1), FactID: factID, KnowerCharacterID: actor, Confidence: 1}
	state.Narrative.Beliefs[testID(4)] = narrative.Belief{ID: testID(4), TimelineID: testID(1), CharacterID: actor, Predicate: "door", Object: json.RawMessage(`"open"`), Stance: narrative.Suspects, Confidence: .2, Status: narrative.FactActive}
	documents, err := StateDocuments(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 3 {
		t.Fatalf("documents=%+v", documents)
	}
	for _, document := range documents {
		if document.Kind == "fact" && (document.OwnerType != "timeline" || strings.Contains(document.Content, "open")) {
			t.Fatal("belief promoted to fact")
		}
		if document.Kind != "fact" && (document.OwnerType != "character" || document.OwnerID != actor) {
			t.Fatal("private owner lost")
		}
		if document.FromSeq != 20 {
			t.Fatal("new document stamped before its exact checkpoint")
		}
	}
	state.Narrative.Knowledge["fact-actor"] = narrative.Knowledge{TimelineID: testID(1), FactID: factID, KnowerCharacterID: actor, InvalidatedAtEventSeq: sequencePtr(20)}
	documents, err = StateDocuments(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 2 {
		t.Fatal("invalidated knowledge remained visible")
	}
	state.LastEventSeq = 21
	next, err := StateDocuments(state)
	if err != nil {
		t.Fatal(err)
	}
	if documents[0].Hash() != next[0].Hash() {
		t.Fatal("unchanged document hash depends on indexing checkpoint")
	}
}

func TestStateDocumentsRejectForeignEntityInsteadOfRelabelingIt(t *testing.T) {
	state := projection.Empty(testTimeline(1))
	state.LastEventSeq = 10
	state.Narrative.Facts[testID(2)] = narrative.Fact{ID: testID(2), TimelineID: testID(99), Predicate: "secret", Object: json.RawMessage(`true`), Status: narrative.FactActive}
	if _, err := StateDocuments(state); err == nil {
		t.Fatal("foreign fact relabeled as local memory")
	}
}
