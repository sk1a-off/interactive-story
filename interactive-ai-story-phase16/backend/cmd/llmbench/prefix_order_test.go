package main

import (
	"bytes"
	"testing"
)

func TestPrefixOrderingPreservesAllValuesAndPrecision(t *testing.T) {
	raw := []byte(`{"action":"Спросить <Миру>","unknown":{"n":9007199254740993,"a":[]},"context":{"previousBeatText":"Меняется","chapterBeatCount":4,"storyBible":{"title":"Архив"},"player":null,"recentBeats":[],"unknown":false},"intent":"Спросить"}`)
	result, err := orderedPromptPrefix(raw)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := semanticPromptJSON(raw)
	after, _ := semanticPromptJSON(result)
	if !bytes.Equal(before, after) || !bytes.Contains(result, []byte("9007199254740993")) {
		t.Fatal("JSON values lost or rounded")
	}
	if !bytes.HasPrefix(result, []byte(`{"context":{"storyBible":`)) || bytes.Index(result, []byte(`"action":`)) < bytes.Index(result, []byte(`"unknown":`)) {
		t.Fatalf("unstable prefix order: %s", result)
	}
	again, err := orderedPromptPrefix(result)
	if err != nil || !bytes.Equal(result, again) {
		t.Fatal("ordering not deterministic/idempotent")
	}
}
func TestPrefixOrderingRejectsAmbiguousOrMalformedObjects(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"x":1,"x":2}`, `{"context":{"x":1,"x":2}}`, `{"x":1} {}`, `{"x":`} {
		if _, err := orderedPromptPrefix([]byte(raw)); err == nil {
			t.Fatalf("bad prompt accepted: %s", raw)
		}
	}
	if _, err := orderedPromptPrefix([]byte(`{"context":null,"x":[null,1,true]}`)); err != nil {
		t.Fatal("non-object optional context altered")
	}
}
func TestStaticFirstExtendsSharedPrefixAcrossChangingTurns(t *testing.T) {
	probes, err := cacheProbes()
	if err != nil || len(probes) != 6 {
		t.Fatal(err)
	}
	for _, role := range []string{"turn_planner", "writer"} {
		var current, ordered [][]byte
		for _, p := range probes {
			if p.Request.Role != role {
				continue
			}
			current = append(current, p.Request.Input)
			value, err := orderedPromptPrefix(p.Request.Input)
			if err != nil {
				t.Fatal(err)
			}
			ordered = append(ordered, value)
		}
		if commonPromptPrefix(ordered) <= commonPromptPrefix(current) {
			t.Fatalf("synthetic stable prefix did not increase for %s", role)
		}
	}
	if commonPromptPrefix(nil) != 0 || commonPromptPrefix([][]byte{[]byte("abc"), []byte("ab")}) != 2 {
		t.Fatal("invalid prefix arithmetic")
	}
}
