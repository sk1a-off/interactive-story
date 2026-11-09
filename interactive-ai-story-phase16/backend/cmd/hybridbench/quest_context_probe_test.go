package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCompactQuestInputPreservesAuthoritativeFields(t *testing.T) {
	input := json.RawMessage(`{"action":"only ask","newBeat":"no purchase","currentObjectives":[{"progress":50}],"quests":[{"parent":"q"}],"context":{"previousBeatText":"paid yesterday","storyBible":{"constraints":"slow growth"},"player":{"name":"hero"},"heroJournal":[{"quantity":10}],"worldRules":["no magic"],"worldResources":[1],"world":{"location":"shop"},"futureField":"keep","visualBible":"large","initialCast":["old"],"recentBeats":["history"]}}`)
	var original map[string]any
	json.Unmarshal(input, &original)
	raw, err := compactQuestInput(input)
	if err != nil {
		t.Fatal(err)
	}
	var actual map[string]any
	json.Unmarshal(raw, &actual)
	state := original["context"].(map[string]any)
	for _, key := range []string{"visualBible", "initialCast", "recentBeats"} {
		delete(state, key)
	}
	if !reflect.DeepEqual(original, actual) {
		t.Fatal("changed fields outside the experiment")
	}
	if len(raw) >= len(input) {
		t.Fatal("context did not shrink")
	}
}

func TestCompactQuestInputRejectsMissingContext(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `{"context":null}`, `{"context":42}`} {
		if _, err := compactQuestInput(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
