package generation

import (
	"errors"
	"testing"

	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

func TestEvidenceQuoteMustComeFromNewBeat(t *testing.T) {
	changes := []journalChange{{Operation: "create", Category: "item", Name: "Паёк", EvidenceQuote: "Спрятав полученный паек за пазуху"}}
	if _, err := prepareJournalEvidence("Герой удержал накренившийся фургон.", nil, changes); !errors.Is(err, ErrRejectedProposal) {
		t.Fatalf("historical evidence accepted: %v", err)
	}
}

func TestJournalNoopUpdateIsDropped(t *testing.T) {
	quantity := 1
	current := []generationtarget.JournalEntry{{ID: "item-1", Category: "item", Name: "Паёк", Description: "Сухой паёк", Quantity: 1, Status: "active", Tags: []string{"еда"}}}
	changes := []journalChange{{Operation: "update", EntryID: "item-1", Name: "Паёк", Description: "Сухой паёк", Quantity: &quantity, Status: "active", Tags: []string{"еда"}, EvidenceQuote: "Герой проверил паёк."}}
	got, err := prepareJournalEvidence("Герой проверил паёк.", current, changes)
	if err != nil || len(got) != 0 {
		t.Fatalf("no-op was not dropped: changes=%#v err=%v", got, err)
	}
}

func TestObjectiveCompletionMustQuoteSuccessCriterion(t *testing.T) {
	current := []generationtarget.Objective{{ID: "quest-1", Status: "active", SuccessCriteria: "Клеть надёжно зафиксирована на платформе"}}
	premature := []objectiveChange{{Operation: "complete", ObjectiveID: "quest-1", EvidenceQuote: "Алекс удержал накренившийся фургон."}}
	if _, err := prepareObjectiveEvidence("Алекс удержал накренившийся фургон.", current, premature); !errors.Is(err, ErrRejectedProposal) {
		t.Fatalf("premature completion accepted: %v", err)
	}
	complete := []objectiveChange{{Operation: "complete", ObjectiveID: "quest-1", EvidenceQuote: "Клеть надёжно зафиксирована новым тросом."}}
	if got, err := prepareObjectiveEvidence("Клеть надёжно зафиксирована новым тросом.", current, complete); err != nil || len(got) != 1 {
		t.Fatalf("supported completion rejected: %#v %v", got, err)
	}
}

func TestObjectiveCriterionSupportsRussianInflection(t *testing.T) {
	if !quoteSupportsCriterion("Клин намертво удержал канат в расщелине.", "Повозки зафиксированы штормовыми канатами") {
		t.Fatal("direct evidence was rejected only because of Russian case inflection")
	}
}

func TestExtractorCanCompleteExplicitlySecuredCableObjective(t *testing.T) {
	parentID, stageID := "parent", "stage"
	current := []generationtarget.Objective{
		{ID: parentID, Scope: "global", Kind: "quest", QuestType: "main", Title: "Переход", SuccessCriteria: "Караван прошёл перевал", Status: "active"},
		{ID: stageID, ParentObjectiveID: parentID, Scope: "minor", Kind: "task", QuestType: "main", Title: "Растяжки", SuccessCriteria: "Повозки зафиксированы штормовыми канатами", Status: "active"},
	}
	beat := "Узел выдержал рывок: клин намертво удержал канат в расщелине."
	changes, err := prepareObjectiveEvidence(beat, current, []objectiveChange{{Operation: "complete", ObjectiveID: stageID, EvidenceQuote: "клин намертво удержал канат в расщелине"}})
	if err != nil {
		t.Fatal(err)
	}
	if events, _, err := validateObjectiveChanges(current, changes); err != nil || len(events) < 1 {
		t.Fatalf("explicitly secured cable completion rejected: events=%v err=%v", events, err)
	}
}

func TestEvidenceNormalizationAllowsWhitespaceButNotParaphrase(t *testing.T) {
	beat := "Герой\n  поднял ключ и положил его в карман."
	if !evidenceBelongsToBeat(beat, "Герой поднял ключ") {
		t.Fatal("normalized verbatim evidence rejected")
	}
	if evidenceBelongsToBeat(beat, "Герой забрал ключ") {
		t.Fatal("paraphrased evidence accepted")
	}
}
