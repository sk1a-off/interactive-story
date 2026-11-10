package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
)

// Experimental only: the production adapter still sends req.Input unchanged.
// Raw values preserve numeric precision, null/empty values and unknown fields.
func orderedPromptPrefix(raw []byte) ([]byte, error) {
	fields, err := promptObject(raw)
	if err != nil {
		return nil, err
	}
	if context := bytes.TrimSpace(fields["context"]); len(context) > 0 && context[0] == '{' {
		nested, err := promptObject(context)
		if err != nil {
			return nil, err
		}
		fields["context"] = marshalPromptFields(nested, []string{"storyBible", "player", "initialCast", "world", "visualBible"}, []string{"previousBeatText", "recentBeats", "nextBeatPosition", "chapterBeatCount", "chapterSceneCount"})
	}
	return marshalPromptFields(fields, []string{"context", "currentCharacters", "currentWorld", "currentJournal", "currentObjectives", "objectives", "quests", "heroJournal", "directorInstructions"}, []string{"pacing", "goal", "directorGoal", "intent", "newBeat", "text", "action"}), nil
}
func promptObject(raw []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errors.New("prefix experiment requires a JSON object")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key := token.(string)
		if _, exists := fields[key]; exists {
			return nil, errors.New("duplicate prompt key")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("trailing prompt JSON")
	}
	return fields, nil
}
func marshalPromptFields(fields map[string]json.RawMessage, first, last []string) []byte {
	var ordered []string
	used, tail := map[string]bool{}, map[string]bool{}
	for _, key := range last {
		tail[key] = true
	}
	for _, key := range first {
		if _, ok := fields[key]; ok {
			ordered = append(ordered, key)
			used[key] = true
		}
	}
	var middle []string
	for key := range fields {
		if !used[key] && !tail[key] {
			middle = append(middle, key)
		}
	}
	sort.Strings(middle)
	ordered = append(ordered, middle...)
	for _, key := range last {
		if _, ok := fields[key]; ok && !used[key] {
			ordered = append(ordered, key)
		}
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for i, key := range ordered {
		if i > 0 {
			out.WriteByte(',')
		}
		encoded, _ := json.Marshal(key)
		out.Write(encoded)
		out.WriteByte(':')
		out.Write(fields[key])
	}
	out.WriteByte('}')
	return out.Bytes()
}
func semanticPromptJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
func commonPromptPrefix(values [][]byte) int {
	if len(values) == 0 {
		return 0
	}
	length := len(values[0])
	for _, value := range values[1:] {
		if len(value) < length {
			length = len(value)
		}
		i := 0
		for i < length && values[0][i] == value[i] {
			i++
		}
		length = i
	}
	return length
}
