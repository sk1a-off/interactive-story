package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/projection"
)

type Document struct {
	Key       string
	Class     string
	Kind      string
	OwnerType string
	OwnerID   id.ID
	Content   string
	FromSeq   int64
}

func (d Document) Hash() string {
	raw, _ := json.Marshal([]any{d.Class, d.Kind, d.OwnerType, d.OwnerID, d.Content})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// StateDocuments serializes exact reconstructed Canon with separate document
// kinds/owners. Ancestor mutable state must never be substituted for this input.
func StateDocuments(state projection.TimelineState) ([]Document, error) {
	if id.ID(state.TimelineID).IsZero() || state.Narrative.TimelineID != id.ID(state.TimelineID) {
		return nil, fmt.Errorf("memory narrative scope mismatch")
	}
	var documents []Document
	add := func(key, kind, ownerType string, ownerID id.ID, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		documents = append(documents, Document{Key: key, Class: "branch_state", Kind: kind, OwnerType: ownerType, OwnerID: ownerID, Content: kind + ": " + string(raw), FromSeq: state.LastEventSeq})
		return nil
	}
	for key, fact := range state.Narrative.Facts {
		if fact.Status != narrative.FactActive || fact.ValidFromSeq > state.LastEventSeq || (fact.InvalidatedAtSeq != nil && *fact.InvalidatedAtSeq <= state.LastEventSeq) {
			continue
		}
		if fact.TimelineID != id.ID(state.TimelineID) {
			return nil, fmt.Errorf("memory fact scope mismatch")
		}
		if err := add("fact:"+key.String(), "fact", "timeline", id.ID(state.TimelineID), fact); err != nil {
			return nil, err
		}
	}
	for key, knowledge := range state.Narrative.Knowledge {
		fact, ok := state.Narrative.Facts[knowledge.FactID]
		if !ok || fact.Status != narrative.FactActive || fact.ValidFromSeq > state.LastEventSeq || knowledge.LearnedAtEventSeq > state.LastEventSeq || (fact.InvalidatedAtSeq != nil && *fact.InvalidatedAtSeq <= state.LastEventSeq) || (knowledge.InvalidatedAtEventSeq != nil && *knowledge.InvalidatedAtEventSeq <= state.LastEventSeq) {
			continue
		}
		if knowledge.TimelineID != id.ID(state.TimelineID) {
			return nil, fmt.Errorf("memory knowledge scope mismatch")
		}
		if err := add("knowledge:"+key, "knowledge", "character", knowledge.KnowerCharacterID, struct {
			Knowledge narrative.Knowledge
			Fact      narrative.Fact
		}{knowledge, fact}); err != nil {
			return nil, err
		}
	}
	for key, belief := range state.Narrative.Beliefs {
		if belief.Status != narrative.FactActive || (belief.SourceEventSeq != nil && *belief.SourceEventSeq > state.LastEventSeq) {
			continue
		}
		if belief.TimelineID != id.ID(state.TimelineID) {
			return nil, fmt.Errorf("memory belief scope mismatch")
		}
		if err := add("belief:"+key.String(), "belief", "character", belief.CharacterID, belief); err != nil {
			return nil, err
		}
	}
	for key, thread := range state.Narrative.Threads {
		if thread.Status != "open" && thread.Status != "dormant" && thread.Status != "resolving" {
			continue
		}
		if thread.TimelineID != id.ID(state.TimelineID) {
			return nil, fmt.Errorf("memory thread scope mismatch")
		}
		if err := add("thread:"+key.String(), "thread", "timeline", id.ID(state.TimelineID), thread); err != nil {
			return nil, err
		}
	}
	for key, chapter := range state.Narrative.Chapters {
		summary := strings.TrimSpace(string(chapter.Summary))
		if summary == "" || summary == "null" || summary == "{}" || summary == "[]" || summary == `""` {
			continue
		}
		if chapter.TimelineID != id.ID(state.TimelineID) {
			return nil, fmt.Errorf("memory chapter scope mismatch")
		}
		if err := add("chapter-summary:"+key.String(), "chapter_summary", "timeline", id.ID(state.TimelineID), chapter); err != nil {
			return nil, err
		}
	}
	for key, rule := range state.Narrative.WorldRules {
		if rule.Status != "established" && rule.Status != "pending" {
			continue
		}
		if rule.TimelineID != id.ID(state.TimelineID) {
			return nil, fmt.Errorf("memory rule scope mismatch")
		}
		if err := add("world-rule:"+key, "world_rule", "timeline", id.ID(state.TimelineID), rule); err != nil {
			return nil, err
		}
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].Key < documents[j].Key })
	for _, document := range documents {
		if document.OwnerID.IsZero() {
			return nil, fmt.Errorf("invalid memory owner for %s", document.Key)
		}
	}
	return documents, nil
}
