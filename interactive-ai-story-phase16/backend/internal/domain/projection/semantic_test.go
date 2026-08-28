package projection

import (
	"encoding/json"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"testing"
	"time"
)

func sid(s string) id.ID { return id.MustParse("00000000-0000-4000-8000-" + s) }
func se(tid timeline.ID, seq int64, typ string, p any) event.StoredEvent {
	b, _ := json.Marshal(p)
	return event.StoredEvent{ID: event.ID(sid("00000000000" + string(rune('0'+seq)))), TimelineID: tid, Seq: seq, Type: typ, SchemaVersion: 1, Payload: b, CreatedAt: time.Unix(seq, 0)}
}
func TestReplayReconstructsOpeningAndGameplayBeat(t *testing.T) {
	tid := timeline.ID(sid("000000000101"))
	ch := sid("000000000102")
	sc := sid("000000000103")
	b1 := sid("000000000104")
	b2 := sid("000000000105")
	events := []event.StoredEvent{
		se(tid, 1, "chapter_created", map[string]any{"chapterId": ch, "number": 1, "title": "One", "goal": "g", "status": "active"}),
		se(tid, 2, "scene_started", map[string]any{"sceneId": sc, "chapterId": ch, "number": 1, "goal": "s", "status": "awaiting_player"}),
		se(tid, 3, "beat_committed", map[string]any{"beatId": b1, "sceneId": sc, "position": 1, "kind": "mixed", "text": "opening", "status": "committed"}),
		se(tid, 4, "choices_ready", map[string]any{"choices": []string{"go"}}),
		se(tid, 5, "player_action_attempted", map[string]any{"text": "go"}),
		se(tid, 6, "beat_committed", map[string]any{"beatId": b2, "sceneId": sc, "position": 2, "kind": "mixed", "text": "next", "status": "committed"}),
		se(tid, 7, "choices_ready", map[string]any{"choices": []string{"again"}}),
	}
	st, e := Replay(tid, events)
	if e != nil {
		t.Fatal(e)
	}
	if len(st.Narrative.Chapters) != 1 || len(st.Narrative.Scenes) != 1 || len(st.Narrative.Beats) != 2 {
		t.Fatalf("replay incomplete: %#v", st.Narrative)
	}
	if string(st.Narrative.Beats[b2].Content) != `{"text":"next"}` {
		t.Fatal("latest beat content lost")
	}
}

func TestReplayMovesCursorAcrossScenesAndChapters(t *testing.T) {
	tid := timeline.ID(sid("000000000111"))
	chapterOne, chapterTwo := sid("000000000112"), sid("000000000113")
	sceneOne, sceneTwo := sid("000000000114"), sid("000000000115")
	instructionID := sid("000000000116")
	state := Empty(tid)
	state.Narrative.Chapters[chapterOne] = narrative.Chapter{ID: chapterOne, TimelineID: id.ID(tid), Number: 1, Status: "active"}
	state.Narrative.Scenes[sceneOne] = narrative.Scene{ID: sceneOne, ChapterID: chapterOne, Number: 1, Status: "awaiting_player"}
	state.Narrative.DirectorInstructions[instructionID] = narrative.DirectorInstruction{ID: instructionID, TimelineID: id.ID(tid), Scope: narrative.ScopeNextBeat, Status: "active"}
	events := []event.StoredEvent{
		se(tid, 1, "scene_completed", map[string]any{"sceneId": sceneOne, "status": "completed"}),
		se(tid, 2, "chapter_completed", map[string]any{"chapterId": chapterOne, "status": "completed"}),
		se(tid, 3, "chapter_created", map[string]any{"chapterId": chapterTwo, "number": 2, "title": "Second", "goal": "advance", "status": "active"}),
		se(tid, 4, "scene_started", map[string]any{"sceneId": sceneTwo, "chapterId": chapterTwo, "number": 1, "goal": "arrive", "status": "awaiting_player"}),
		se(tid, 5, "director_instructions_expired", map[string]any{"instructionIds": []id.ID{instructionID}}),
	}
	var err error
	for _, stored := range events {
		state, err = Apply(state, stored)
		if err != nil {
			t.Fatal(err)
		}
	}
	if state.Narrative.Chapters[chapterOne].Status != "completed" || state.Narrative.Scenes[sceneOne].Status != "completed" {
		t.Fatal("previous lifecycle remained active")
	}
	if state.Narrative.Chapters[chapterTwo].Status != "active" || state.Narrative.Scenes[sceneTwo].Status != "awaiting_player" {
		t.Fatal("new lifecycle was not activated")
	}
	if state.Narrative.DirectorInstructions[instructionID].Status != "expired" {
		t.Fatal("scoped director instruction did not expire")
	}
}

func TestReplayHandlesEventSourcedAndLegacyDirectorInstructions(t *testing.T) {
	tid := timeline.ID(sid("000000000117"))
	instructionID := sid("000000000118")
	legacyMissingID := sid("000000000119")
	events := []event.StoredEvent{
		se(tid, 1, "director_instruction_added", map[string]any{"instructionId": instructionID, "timelineId": tid, "text": "Вести к башне", "scope": "next_beat", "priority": "high", "status": "active", "createdAtSeq": 1}),
		se(tid, 2, "director_instructions_expired", map[string]any{"instructionIds": []id.ID{instructionID, legacyMissingID}}),
	}
	state, err := Replay(tid, events)
	if err != nil {
		t.Fatal(err)
	}
	instruction := state.Narrative.DirectorInstructions[instructionID]
	if instruction.Text != "Вести к башне" || instruction.Status != "expired" || instruction.Priority != narrative.PriorityHigh {
		t.Fatalf("instruction lifecycle was not replayed: %+v", instruction)
	}
}

func TestReplayPersistsGeneratedWorldEntities(t *testing.T) {
	tid := timeline.ID(sid("000000000121"))
	characterID, locationID := sid("000000000122"), sid("000000000123")
	events := []event.StoredEvent{
		se(tid, 1, "character_world_upserted", map[string]any{"characterId": characterID, "name": "Смотритель Илья", "age": 43, "kind": "persistent_npc", "role": "хранитель", "mood": "насторожен", "currentGoal": "закрыть архив", "active": true}),
		se(tid, 2, "location_world_upserted", map[string]any{"locationId": locationID, "name": "Закрытый архив", "description": "Подземное хранилище", "visualAnchorEn": "underground archive vault", "active": true}),
	}
	state, err := Replay(tid, events)
	if err != nil {
		t.Fatal(err)
	}
	character := state.Narrative.Characters[characterID]
	location := state.Narrative.Locations[locationID]
	if !character.Active || character.Mood != "насторожен" || character.CurrentGoal != "закрыть архив" || character.Version != 1 {
		t.Fatalf("generated character was not replayed: %+v", character)
	}
	if !location.Active || location.Name != "Закрытый архив" || location.Description != "Подземное хранилище" || location.Version != 1 {
		t.Fatalf("generated location was not replayed: %+v", location)
	}
}
func TestDirectorReplayFromSnapshotBase(t *testing.T) {
	tid := timeline.ID(sid("000000000201"))
	char := sid("000000000202")
	item := sid("000000000203")
	fact := sid("000000000204")
	st := Empty(tid)
	st.Narrative.Items[item] = narrative.ItemState{TimelineID: id.ID(tid), ItemID: item, Condition: "old", Version: 1}
	ev := []event.StoredEvent{
		se(tid, 1, "director_exact_edit_applied", map[string]any{"commandType": "set_stat", "targetType": "stat", "after": map[string]any{"OwnerType": "character", "OwnerID": char, "Key": "trust", "ValueType": "number", "Value": 55, "EvolutionMode": "immediate"}}),
		se(tid, 2, "director_exact_edit_applied", map[string]any{"commandType": "upsert_fact", "targetType": "fact", "after": map[string]any{"ID": fact, "SubjectType": "world", "SubjectID": sid("000000000205"), "Predicate": "door_locked", "Object": true}}),
		se(tid, 3, "director_exact_edit_applied", map[string]any{"commandType": "grant_knowledge", "targetType": "knowledge", "after": map[string]any{"FactID": fact, "CharacterID": char, "Confidence": 1}}),
		se(tid, 4, "director_exact_edit_applied", map[string]any{"commandType": "transfer_item", "targetType": "item", "after": map[string]any{"ItemID": item, "OwnerCharacterID": char, "LocationID": "", "Condition": "good"}}),
	}
	var e error
	for _, x := range ev {
		st, e = Apply(st, x)
		if e != nil {
			t.Fatal(e)
		}
	}
	if len(st.Narrative.Stats) != 1 || len(st.Narrative.Facts) != 1 || len(st.Narrative.Knowledge) != 1 {
		t.Fatal("director semantics not reconstructed")
	}
	if st.Narrative.Items[item].OwnerCharacterID != char {
		t.Fatal("item transfer not replayed")
	}
}

func TestObjectiveEventsReplayAsBranchLocalStoryThreads(t *testing.T) {
	tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000501"))
	objectiveID := id.MustParse("00000000-0000-4000-8000-000000000502")
	created := se(tid, 1, "objective_created", map[string]any{"objectiveId": objectiveID, "scope": "minor", "title": "Find the archive key", "description": "Search the office", "successCriteria": "The key is in the player's possession", "status": "active", "progress": 20, "evidence": "A matching key tag was found"})
	updated := se(tid, 2, "objective_updated", map[string]any{"objectiveId": objectiveID, "scope": "minor", "title": "Find the archive key", "description": "Search the office", "successCriteria": "The key is in the player's possession", "status": "completed", "progress": 100, "evidence": "The player took the archive key"})
	state, err := Replay(tid, []event.StoredEvent{created, updated})
	if err != nil {
		t.Fatal(err)
	}
	objective, ok := state.Narrative.Threads[objectiveID]
	if !ok || objective.Status != narrative.ThreadClosed || objective.LastTouchedAtSeq != 2 {
		t.Fatalf("objective projection mismatch: %+v", objective)
	}
	var metadata struct {
		Kind     string `json:"kind"`
		Scope    string `json:"scope"`
		Progress int    `json:"progress"`
		Evidence string `json:"evidence"`
	}
	if err := json.Unmarshal(objective.Metadata, &metadata); err != nil || metadata.Kind != "objective" || metadata.Scope != "minor" || metadata.Progress != 100 || metadata.Evidence == "" {
		t.Fatalf("objective metadata mismatch: %s", objective.Metadata)
	}
}

func TestQuestStageReplayPreservesParentAndKind(t *testing.T) {
	tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000511"))
	questID := id.MustParse("00000000-0000-4000-8000-000000000512")
	stageID := id.MustParse("00000000-0000-4000-8000-000000000513")
	quest := se(tid, 1, "objective_created", map[string]any{"objectiveId": questID, "scope": "global", "kind": "quest", "questType": "side", "title": "Stop the storm", "successCriteria": "The storm ends", "status": "active", "progress": 0})
	stage := se(tid, 2, "objective_created", map[string]any{"objectiveId": stageID, "parentObjectiveId": questID, "scope": "minor", "kind": "event", "questType": "side", "title": "Wait for the eclipse", "successCriteria": "The eclipse begins", "status": "active", "progress": 0})
	state, err := Replay(tid, []event.StoredEvent{quest, stage})
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		ParentObjectiveID id.ID  `json:"parentObjectiveId"`
		ObjectiveKind     string `json:"objectiveKind"`
		QuestType         string `json:"questType"`
	}
	if err = json.Unmarshal(state.Narrative.Threads[stageID].Metadata, &metadata); err != nil || metadata.ParentObjectiveID != questID || metadata.ObjectiveKind != "event" || metadata.QuestType != "side" {
		t.Fatalf("quest stage hierarchy was lost during replay: %s", state.Narrative.Threads[stageID].Metadata)
	}
}

func TestHeroJournalReplayKeepsAbilityAndRemovedInventoryHistory(t *testing.T) {
	tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000521"))
	abilityID := id.MustParse("00000000-0000-4000-8000-000000000522")
	itemID := id.MustParse("00000000-0000-4000-8000-000000000523")
	events := []event.StoredEvent{
		se(tid, 1, "journal_entry_created", map[string]any{"entryId": abilityID, "category": "ability", "name": "Эхо-память", "description": "Слышать остаточный звук", "quantity": 0, "level": "нестабильно", "status": "active", "evidence": "Способность проявилась", "tags": []string{"анализ"}}),
		se(tid, 2, "journal_entry_created", map[string]any{"entryId": itemID, "category": "item", "name": "Медпакет", "description": "Полевой набор", "quantity": 1, "status": "active", "evidence": "Найден в шкафчике"}),
		se(tid, 3, "journal_entry_updated", map[string]any{"entryId": itemID, "category": "item", "name": "Медпакет", "description": "Полевой набор", "quantity": 0, "status": "inactive", "evidence": "Использован при перевязке"}),
	}
	state, err := Replay(tid, events)
	if err != nil {
		t.Fatal(err)
	}
	ability := state.Narrative.Threads[abilityID]
	item := state.Narrative.Threads[itemID]
	if ability.Status != narrative.ThreadOpen || ability.Title != "Эхо-память" || ability.LastTouchedAtSeq != 1 {
		t.Fatalf("ability projection mismatch: %+v", ability)
	}
	if item.Status != narrative.ThreadClosed || item.IntroducedAtSeq != 2 || item.LastTouchedAtSeq != 3 {
		t.Fatalf("inventory history mismatch: %+v", item)
	}
	var metadata struct {
		Kind     string `json:"kind"`
		Category string `json:"category"`
		Quantity int    `json:"quantity"`
		Evidence string `json:"evidence"`
	}
	if err = json.Unmarshal(item.Metadata, &metadata); err != nil || metadata.Kind != "hero_journal" || metadata.Category != "item" || metadata.Quantity != 0 || metadata.Evidence == "" {
		t.Fatalf("journal metadata mismatch: %s", item.Metadata)
	}
}

func TestDirectorCanCorrectHeroJournalAndReactivateInstruction(t *testing.T) {
	tid := timeline.ID(id.MustParse("00000000-0000-4000-8000-000000000531"))
	entryID := id.MustParse("00000000-0000-4000-8000-000000000532")
	instructionID := id.MustParse("00000000-0000-4000-8000-000000000533")
	state := Empty(tid)
	state.Narrative.DirectorInstructions[instructionID] = narrative.DirectorInstruction{ID: instructionID, TimelineID: id.ID(tid), Text: "Старое направление", Scope: narrative.ScopeNextBeat, Priority: narrative.PriorityNormal, Status: "expired", CreatedAtSeq: 1}
	events := []event.StoredEvent{
		se(tid, 1, "director_exact_edit_applied", map[string]any{"commandType": "upsert_journal_entry", "targetType": "journal_entry", "after": map[string]any{"EntryID": entryID, "Category": "currency", "Name": "Серебряные монеты", "Description": "Текущий баланс", "Quantity": 27, "Status": "active", "Evidence": "Исправлено режиссёром", "Tags": []string{"деньги"}}}),
		se(tid, 2, "director_exact_edit_applied", map[string]any{"commandType": "update_instruction", "targetType": "instruction", "after": map[string]any{"InstructionID": instructionID, "Text": "Исправленное направление", "Scope": "chapter", "Priority": "high", "Status": "active", "CreatedAtSeq": 24}}),
	}
	var err error
	for _, stored := range events {
		state, err = Apply(state, stored)
		if err != nil {
			t.Fatal(err)
		}
	}
	entry := state.Narrative.Threads[entryID]
	if entry.Title != "Серебряные монеты" || entry.Status != narrative.ThreadOpen {
		t.Fatalf("director journal correction was not replayed: %+v", entry)
	}
	var metadata struct {
		Category string `json:"category"`
		Quantity int    `json:"quantity"`
	}
	if err = json.Unmarshal(entry.Metadata, &metadata); err != nil || metadata.Category != "currency" || metadata.Quantity != 27 {
		t.Fatalf("director journal metadata mismatch: %s", entry.Metadata)
	}
	instruction := state.Narrative.DirectorInstructions[instructionID]
	if instruction.Text != "Исправленное направление" || instruction.Status != "active" || instruction.Scope != narrative.ScopeChapter || instruction.Priority != narrative.PriorityHigh || instruction.CreatedAtSeq != 24 {
		t.Fatalf("director instruction correction was not replayed: %+v", instruction)
	}
}

func TestUnknownSemanticEventFailsReplay(t *testing.T) {
	tid := timeline.ID(sid("000000000301"))
	_, e := Replay(tid, []event.StoredEvent{se(tid, 1, "mystery_event", map[string]any{})})
	if e != ErrUnsupportedSemanticEvent {
		t.Fatalf("unknown event silently accepted: %v", e)
	}
}
