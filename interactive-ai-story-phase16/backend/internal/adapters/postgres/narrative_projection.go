package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
)

type narrativeProjectionRepository struct{ tx pgx.Tx }

func (r narrativeProjectionRepository) Export(ctx context.Context, tid timeline.ID) (narrative.State, error) {
	state := narrative.NewState(id.ID(tid))
	if err := r.exportChapters(ctx, tid, &state); err != nil {
		return narrative.State{}, err
	}
	if err := r.exportScenes(ctx, tid, &state); err != nil {
		return narrative.State{}, err
	}
	if err := r.exportBeats(ctx, tid, &state); err != nil {
		return narrative.State{}, err
	}
	if err := r.exportCharacters(ctx, tid, &state); err != nil {
		return narrative.State{}, err
	}
	if err := r.exportLocations(ctx, tid, &state); err != nil {
		return narrative.State{}, err
	}
	if err := r.exportRelationships(ctx, tid, &state); err != nil {
		return narrative.State{}, err
	}
	if err := r.exportStats(ctx, tid, &state); err != nil {
		return narrative.State{}, err
	}
	if err := r.exportFacts(ctx, tid, &state); err != nil {
		return narrative.State{}, err
	}
	if err := r.exportKnowledge(ctx, tid, &state); err != nil {
		return narrative.State{}, err
	}
	if err := r.exportBeliefs(ctx, tid, &state); err != nil {
		return narrative.State{}, err
	}
	if err := r.exportWorldCanon(ctx, tid, &state); err != nil {
		return narrative.State{}, err
	}
	if err := r.exportItems(ctx, tid, &state); err != nil {
		return narrative.State{}, err
	}
	if err := r.exportThreads(ctx, tid, &state); err != nil {
		return narrative.State{}, err
	}
	if err := r.exportDirector(ctx, tid, &state); err != nil {
		return narrative.State{}, err
	}
	return state, nil
}

func rows(ctx context.Context, tx pgx.Tx, q string, args ...any) (pgx.Rows, error) {
	r, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (r narrativeProjectionRepository) exportChapters(ctx context.Context, tid timeline.ID, s *narrative.State) error {
	rs, err := rows(ctx, r.tx, `SELECT id,timeline_id,number,title,goal,tone,status,COALESCE(summary,'null'::jsonb) FROM chapters WHERE timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export chapters: %w", err)
	}
	defer rs.Close()
	for rs.Next() {
		var v narrative.Chapter
		var raw []byte
		if err := rs.Scan(&v.ID, &v.TimelineID, &v.Number, &v.Title, &v.Goal, &v.Tone, &v.Status, &raw); err != nil {
			return err
		}
		v.Summary = json.RawMessage(raw)
		s.Chapters[v.ID] = v
	}
	return rs.Err()
}
func (r narrativeProjectionRepository) exportScenes(ctx context.Context, tid timeline.ID, s *narrative.State) error {
	rs, err := rows(ctx, r.tx, `SELECT s.id,s.chapter_id,COALESCE(s.location_id::text,''),s.number,s.story_time,s.mood,s.goal,s.status FROM scenes s JOIN chapters c ON c.id=s.chapter_id WHERE c.timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export scenes: %w", err)
	}
	defer rs.Close()
	for rs.Next() {
		var v narrative.Scene
		var raw []byte
		if err := rs.Scan(&v.ID, &v.ChapterID, &v.LocationID, &v.Number, &raw, &v.Mood, &v.Goal, &v.Status); err != nil {
			return err
		}
		v.StoryTime = json.RawMessage(raw)
		s.Scenes[v.ID] = v
	}
	return rs.Err()
}
func (r narrativeProjectionRepository) exportBeats(ctx context.Context, tid timeline.ID, s *narrative.State) error {
	rs, err := rows(ctx, r.tx, `SELECT b.id,b.scene_id,b.position,b.kind,b.content,b.status,b.is_active,COALESCE(b.generation_id::text,'') FROM beats b JOIN scenes sc ON sc.id=b.scene_id JOIN chapters c ON c.id=sc.chapter_id WHERE c.timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export beats: %w", err)
	}
	defer rs.Close()
	for rs.Next() {
		var v narrative.Beat
		var raw []byte
		if err := rs.Scan(&v.ID, &v.SceneID, &v.Position, &v.Kind, &raw, &v.Status, &v.IsActive, &v.GenerationID); err != nil {
			return err
		}
		v.Content = json.RawMessage(raw)
		s.Beats[v.ID] = v
	}
	return rs.Err()
}
func (r narrativeProjectionRepository) exportCharacters(ctx context.Context, tid timeline.ID, s *narrative.State) error {
	rs, err := rows(ctx, r.tx, `SELECT timeline_id,character_id,COALESCE(location_id::text,''),mood,current_goal,active,current_appearance,director_profile,version FROM character_states WHERE timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export character states: %w", err)
	}
	defer rs.Close()
	for rs.Next() {
		var v narrative.CharacterState
		var appearance, profile []byte
		if err := rs.Scan(&v.TimelineID, &v.CharacterID, &v.LocationID, &v.Mood, &v.CurrentGoal, &v.Active, &appearance, &profile, &v.Version); err != nil {
			return err
		}
		v.Appearance = json.RawMessage(appearance)
		v.Profile = json.RawMessage(profile)
		s.Characters[v.CharacterID] = v
	}
	return rs.Err()
}
func (r narrativeProjectionRepository) exportLocations(ctx context.Context, tid timeline.ID, s *narrative.State) error {
	rs, err := rows(ctx, r.tx, `SELECT timeline_id,location_id,name_override,description_override,visual_profile_override,active,version FROM location_states WHERE timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export location states: %w", err)
	}
	defer rs.Close()
	for rs.Next() {
		var v narrative.LocationState
		var visual []byte
		if err := rs.Scan(&v.TimelineID, &v.LocationID, &v.Name, &v.Description, &visual, &v.Active, &v.Version); err != nil {
			return err
		}
		v.VisualProfile = json.RawMessage(visual)
		s.Locations[v.LocationID] = v
	}
	return rs.Err()
}
func (r narrativeProjectionRepository) exportRelationships(ctx context.Context, tid timeline.ID, s *narrative.State) error {
	rs, err := rows(ctx, r.tx, `SELECT id,timeline_id,from_character_id,to_character_id,status,summary FROM relationships WHERE timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export relationships: %w", err)
	}
	defer rs.Close()
	for rs.Next() {
		var v narrative.Relationship
		if err := rs.Scan(&v.ID, &v.TimelineID, &v.FromCharacterID, &v.ToCharacterID, &v.Status, &v.Summary); err != nil {
			return err
		}
		s.Relationships[v.ID] = v
	}
	return rs.Err()
}
func (r narrativeProjectionRepository) exportStats(ctx context.Context, tid timeline.ID, s *narrative.State) error {
	rs, err := rows(ctx, r.tx, `SELECT timeline_id,owner_type,owner_id,key,value_type,value,COALESCE(target_value,'null'::jsonb),evolution_mode,evolution_meta FROM stats WHERE timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export stats: %w", err)
	}
	defer rs.Close()
	for rs.Next() {
		var v narrative.Stat
		var value, target []byte
		if err := rs.Scan(&v.TimelineID, &v.OwnerType, &v.OwnerID, &v.Key, &v.ValueType, &value, &target, &v.EvolutionMode, &v.EvolutionMeta); err != nil {
			return err
		}
		v.Value = json.RawMessage(value)
		v.TargetValue = json.RawMessage(target)
		s.Stats[string(v.OwnerType)+"|"+v.OwnerID.String()+"|"+v.Key] = v
	}
	return rs.Err()
}
func (r narrativeProjectionRepository) exportFacts(ctx context.Context, tid timeline.ID, s *narrative.State) error {
	rs, err := rows(ctx, r.tx, `SELECT id,timeline_id,subject_type,subject_id,predicate,object,status,COALESCE(source_event_id::text,''),valid_from_seq,invalidated_at_seq FROM facts WHERE timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export facts: %w", err)
	}
	defer rs.Close()
	for rs.Next() {
		var v narrative.Fact
		var raw []byte
		if err := rs.Scan(&v.ID, &v.TimelineID, &v.SubjectType, &v.SubjectID, &v.Predicate, &raw, &v.Status, &v.SourceEventID, &v.ValidFromSeq, &v.InvalidatedAtSeq); err != nil {
			return err
		}
		v.Object = json.RawMessage(raw)
		s.Facts[v.ID] = v
	}
	return rs.Err()
}
func (r narrativeProjectionRepository) exportKnowledge(ctx context.Context, tid timeline.ID, s *narrative.State) error {
	rs, err := rows(ctx, r.tx, `SELECT timeline_id,fact_id,knower_character_id,confidence,learned_at_event_seq,invalidated_at_event_seq FROM knowledge_entries WHERE timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export knowledge: %w", err)
	}
	defer rs.Close()
	for rs.Next() {
		var v narrative.Knowledge
		if err := rs.Scan(&v.TimelineID, &v.FactID, &v.KnowerCharacterID, &v.Confidence, &v.LearnedAtEventSeq, &v.InvalidatedAtEventSeq); err != nil {
			return err
		}
		s.Knowledge[v.FactID.String()+"|"+v.KnowerCharacterID.String()] = v
	}
	return rs.Err()
}
func (r narrativeProjectionRepository) exportBeliefs(ctx context.Context, tid timeline.ID, s *narrative.State) error {
	rs, err := rows(ctx, r.tx, `SELECT id,timeline_id,character_id,subject_type,COALESCE(subject_id::text,''),predicate,object,stance,confidence,status,source_event_seq FROM character_beliefs WHERE timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export beliefs: %w", err)
	}
	defer rs.Close()
	for rs.Next() {
		var v narrative.Belief
		var raw []byte
		if err := rs.Scan(&v.ID, &v.TimelineID, &v.CharacterID, &v.SubjectType, &v.SubjectID, &v.Predicate, &raw, &v.Stance, &v.Confidence, &v.Status, &v.SourceEventSeq); err != nil {
			return err
		}
		v.Object = json.RawMessage(raw)
		s.Beliefs[v.ID] = v
	}
	return rs.Err()
}
func (r narrativeProjectionRepository) exportWorldCanon(ctx context.Context, tid timeline.ID, s *narrative.State) error {
	s.EnsureWorldMaps()
	systems, err := rows(ctx, r.tx, `SELECT timeline_id,system_id,name,kind,description,resources,status,version FROM world_systems WHERE timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export world systems: %w", err)
	}
	for systems.Next() {
		var v narrative.WorldSystem
		var resources []byte
		if err = systems.Scan(&v.TimelineID, &v.ID, &v.Name, &v.Kind, &v.Description, &resources, &v.Status, &v.Version); err != nil {
			systems.Close()
			return err
		}
		v.Resources = json.RawMessage(resources)
		s.WorldSystems[v.ID] = v
	}
	if err = systems.Err(); err != nil {
		systems.Close()
		return err
	}
	systems.Close()
	rules, err := rows(ctx, r.tx, `SELECT timeline_id,rule_id,system_id,title,category,severity,statement,preconditions,costs,forbidden_results,exceptions,tags,visibility,status,exception_of,source,evidence,version,established_at_seq FROM world_rules WHERE timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export world rules: %w", err)
	}
	for rules.Next() {
		var v narrative.WorldRule
		var pre, costs, forbidden, exceptions, tags []byte
		if err = rules.Scan(&v.TimelineID, &v.ID, &v.SystemID, &v.Title, &v.Category, &v.Severity, &v.Statement, &pre, &costs, &forbidden, &exceptions, &tags, &v.Visibility, &v.Status, &v.ExceptionOf, &v.Source, &v.Evidence, &v.Version, &v.EstablishedAtSeq); err != nil {
			rules.Close()
			return err
		}
		v.Preconditions = json.RawMessage(pre)
		v.Costs = json.RawMessage(costs)
		v.ForbiddenResults = json.RawMessage(forbidden)
		v.Exceptions = json.RawMessage(exceptions)
		v.Tags = json.RawMessage(tags)
		s.WorldRules[v.ID] = v
	}
	if err = rules.Err(); err != nil {
		rules.Close()
		return err
	}
	rules.Close()
	resources, err := rows(ctx, r.tx, `SELECT timeline_id,resource_id,system_id,owner_type,COALESCE(owner_id::text,''),owner_key,name,unit,current_value::float8,min_value::float8,max_value::float8,visibility,version FROM world_resource_states WHERE timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export world resources: %w", err)
	}
	for resources.Next() {
		var v narrative.WorldResourceState
		if err = resources.Scan(&v.TimelineID, &v.ResourceID, &v.SystemID, &v.OwnerType, &v.OwnerID, &v.OwnerKey, &v.Name, &v.Unit, &v.Current, &v.Minimum, &v.Maximum, &v.Visibility, &v.Version); err != nil {
			resources.Close()
			return err
		}
		s.WorldResources[v.ResourceID+"|"+v.OwnerType+"|"+v.OwnerKey] = v
	}
	if err = resources.Err(); err != nil {
		resources.Close()
		return err
	}
	resources.Close()
	return nil
}
func (r narrativeProjectionRepository) exportItems(ctx context.Context, tid timeline.ID, s *narrative.State) error {
	rs, err := rows(ctx, r.tx, `SELECT timeline_id,item_id,COALESCE(owner_character_id::text,''),COALESCE(location_id::text,''),condition,state,version FROM item_states WHERE timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export item states: %w", err)
	}
	defer rs.Close()
	for rs.Next() {
		var v narrative.ItemState
		var raw []byte
		if err := rs.Scan(&v.TimelineID, &v.ItemID, &v.OwnerCharacterID, &v.LocationID, &v.Condition, &raw, &v.Version); err != nil {
			return err
		}
		v.State = json.RawMessage(raw)
		s.Items[v.ItemID] = v
	}
	return rs.Err()
}
func (r narrativeProjectionRepository) exportThreads(ctx context.Context, tid timeline.ID, s *narrative.State) error {
	rs, err := rows(ctx, r.tx, `SELECT id,timeline_id,title,summary,importance,status,metadata,introduced_at_seq,last_touched_at_seq FROM story_threads WHERE timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export threads: %w", err)
	}
	defer rs.Close()
	for rs.Next() {
		var v narrative.StoryThread
		var raw []byte
		if err := rs.Scan(&v.ID, &v.TimelineID, &v.Title, &v.Summary, &v.Importance, &v.Status, &raw, &v.IntroducedAtSeq, &v.LastTouchedAtSeq); err != nil {
			return err
		}
		v.Metadata = json.RawMessage(raw)
		s.Threads[v.ID] = v
	}
	return rs.Err()
}
func (r narrativeProjectionRepository) exportDirector(ctx context.Context, tid timeline.ID, s *narrative.State) error {
	rs, err := rows(ctx, r.tx, `SELECT id,timeline_id,instruction_text,scope,priority,status,created_at_seq,COALESCE(expires_at,'null'::jsonb) FROM director_instructions WHERE timeline_id=$1`, tid)
	if err != nil {
		return fmt.Errorf("export director: %w", err)
	}
	defer rs.Close()
	for rs.Next() {
		var v narrative.DirectorInstruction
		var raw []byte
		if err := rs.Scan(&v.ID, &v.TimelineID, &v.Text, &v.Scope, &v.Priority, &v.Status, &v.CreatedAtSeq, &raw); err != nil {
			return err
		}
		v.ExpiresAt = json.RawMessage(raw)
		s.DirectorInstructions[v.ID] = v
	}
	return rs.Err()
}

func nilID(v id.ID) any {
	if v.IsZero() {
		return nil
	}
	return v
}
func jsonOr(v json.RawMessage, fallback string) any {
	if len(v) == 0 {
		return []byte(fallback)
	}
	return []byte(v)
}
func nullableJSON(v json.RawMessage) any {
	if len(v) == 0 || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
		return nil
	}
	return []byte(v)
}

func (r narrativeProjectionRepository) Materialize(ctx context.Context, s narrative.State) error {
	for _, v := range s.Chapters {
		if _, err := r.tx.Exec(ctx, `INSERT INTO chapters(id,timeline_id,number,title,goal,tone,status,summary,ended_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,CASE WHEN $7 IN('completed','superseded') THEN now() ELSE NULL END) ON CONFLICT(id) DO UPDATE SET number=EXCLUDED.number,title=EXCLUDED.title,goal=EXCLUDED.goal,tone=EXCLUDED.tone,status=EXCLUDED.status,summary=EXCLUDED.summary,ended_at=CASE WHEN EXCLUDED.status IN('completed','superseded') THEN COALESCE(chapters.ended_at,now()) ELSE NULL END`, v.ID, v.TimelineID, v.Number, v.Title, v.Goal, v.Tone, v.Status, nullableJSON(v.Summary)); err != nil {
			return fmt.Errorf("materialize chapter: %w", err)
		}
	}
	for _, v := range s.Scenes {
		if _, err := r.tx.Exec(ctx, `INSERT INTO scenes(id,chapter_id,number,location_id,story_time,mood,goal,status,ended_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,CASE WHEN $8 IN('completed','superseded') THEN now() ELSE NULL END) ON CONFLICT(id) DO UPDATE SET location_id=EXCLUDED.location_id,story_time=EXCLUDED.story_time,mood=EXCLUDED.mood,goal=EXCLUDED.goal,status=EXCLUDED.status,ended_at=CASE WHEN EXCLUDED.status IN('completed','superseded') THEN COALESCE(scenes.ended_at,now()) ELSE NULL END`, v.ID, v.ChapterID, v.Number, nilID(v.LocationID), jsonOr(v.StoryTime, "{}"), v.Mood, v.Goal, v.Status); err != nil {
			return fmt.Errorf("materialize scene: %w", err)
		}
	}
	for _, v := range s.Beats {
		if _, err := r.tx.Exec(ctx, `INSERT INTO beats(id,scene_id,position,kind,content,status,is_active,generation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET kind=EXCLUDED.kind,content=EXCLUDED.content,status=EXCLUDED.status,is_active=EXCLUDED.is_active,generation_id=EXCLUDED.generation_id`, v.ID, v.SceneID, v.Position, v.Kind, jsonOr(v.Content, "{}"), v.Status, v.IsActive, nilID(v.GenerationID)); err != nil {
			return fmt.Errorf("materialize beat: %w", err)
		}
	}
	for _, v := range s.Characters {
		var profile struct {
			Name, Kind, Role, Personality, Relationship, VisualAnchorEn string
			Age                                                         int
		}
		_ = json.Unmarshal(v.Profile, &profile)
		if strings.TrimSpace(profile.Name) != "" {
			if profile.Kind == "" {
				profile.Kind = "persistent_npc"
			}
			if profile.Age < 1 || profile.Age > 150 {
				profile.Age = 18
			}
			core, _ := json.Marshal(map[string]any{"role": profile.Role, "personality": profile.Personality, "relationship": profile.Relationship})
			visual, _ := json.Marshal(map[string]any{"anchorEn": profile.VisualAnchorEn})
			if _, err := r.tx.Exec(ctx, `INSERT INTO characters(id,story_id,kind,name,adult_age,core,visual_profile)
SELECT $1,t.story_id,$3,$4,$5,$6,$7 FROM timelines t WHERE t.id=$2
ON CONFLICT(id) DO NOTHING`, v.CharacterID, v.TimelineID, profile.Kind, profile.Name, profile.Age, core, visual); err != nil {
				return fmt.Errorf("materialize character identity: %w", err)
			}
		}
		if _, err := r.tx.Exec(ctx, `INSERT INTO character_states(timeline_id,character_id,location_id,mood,current_goal,active,current_appearance,director_profile,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(timeline_id,character_id) DO UPDATE SET location_id=EXCLUDED.location_id,mood=EXCLUDED.mood,current_goal=EXCLUDED.current_goal,active=EXCLUDED.active,current_appearance=EXCLUDED.current_appearance,director_profile=EXCLUDED.director_profile,version=EXCLUDED.version,updated_at=now()`, v.TimelineID, v.CharacterID, nilID(v.LocationID), v.Mood, v.CurrentGoal, v.Active, jsonOr(v.Appearance, "{}"), jsonOr(v.Profile, "{}"), v.Version); err != nil {
			return fmt.Errorf("materialize character state: %w", err)
		}
	}
	for _, v := range s.Locations {
		if strings.TrimSpace(v.Name) != "" {
			if _, err := r.tx.Exec(ctx, `INSERT INTO locations(id,story_id,name,description,visual_profile)
SELECT $1,t.story_id,$3,$4,$5 FROM timelines t WHERE t.id=$2
ON CONFLICT(id) DO NOTHING`, v.LocationID, v.TimelineID, v.Name, v.Description, jsonOr(v.VisualProfile, "{}")); err != nil {
				return fmt.Errorf("materialize location identity: %w", err)
			}
		}
		if _, err := r.tx.Exec(ctx, `INSERT INTO location_states(timeline_id,location_id,name_override,description_override,visual_profile_override,active,version) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(timeline_id,location_id) DO UPDATE SET name_override=EXCLUDED.name_override,description_override=EXCLUDED.description_override,visual_profile_override=EXCLUDED.visual_profile_override,active=EXCLUDED.active,version=EXCLUDED.version,updated_at=now()`, v.TimelineID, v.LocationID, v.Name, v.Description, jsonOr(v.VisualProfile, "{}"), v.Active, v.Version); err != nil {
			return fmt.Errorf("materialize location state: %w", err)
		}
	}
	for _, v := range s.Relationships {
		if _, err := r.tx.Exec(ctx, `INSERT INTO relationships(id,timeline_id,from_character_id,to_character_id,status,summary) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,summary=EXCLUDED.summary`, v.ID, v.TimelineID, v.FromCharacterID, v.ToCharacterID, v.Status, v.Summary); err != nil {
			return fmt.Errorf("materialize relationship: %w", err)
		}
	}
	for _, v := range s.Stats {
		evolutionMode := v.EvolutionMode
		if evolutionMode == "" {
			evolutionMode = "none"
		}
		if _, err := r.tx.Exec(ctx, `INSERT INTO stats(timeline_id,owner_type,owner_id,key,value_type,value,target_value,evolution_mode,evolution_meta) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(timeline_id,owner_type,owner_id,key) DO UPDATE SET value_type=EXCLUDED.value_type,value=EXCLUDED.value,target_value=EXCLUDED.target_value,evolution_mode=EXCLUDED.evolution_mode,evolution_meta=EXCLUDED.evolution_meta,updated_at=now()`, v.TimelineID, v.OwnerType, v.OwnerID, v.Key, v.ValueType, jsonOr(v.Value, "null"), nullableJSON(v.TargetValue), evolutionMode, jsonOr(v.EvolutionMeta, "{}")); err != nil {
			return fmt.Errorf("materialize stat: %w", err)
		}
	}
	for _, v := range s.Facts {
		if _, err := r.tx.Exec(ctx, `INSERT INTO facts(id,timeline_id,subject_type,subject_id,predicate,object,status,source_event_id,valid_from_seq,invalidated_at_seq) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO UPDATE SET subject_type=EXCLUDED.subject_type,subject_id=EXCLUDED.subject_id,predicate=EXCLUDED.predicate,object=EXCLUDED.object,status=EXCLUDED.status,source_event_id=EXCLUDED.source_event_id,valid_from_seq=EXCLUDED.valid_from_seq,invalidated_at_seq=EXCLUDED.invalidated_at_seq`, v.ID, v.TimelineID, v.SubjectType, v.SubjectID, v.Predicate, jsonOr(v.Object, "null"), v.Status, nilID(v.SourceEventID), v.ValidFromSeq, v.InvalidatedAtSeq); err != nil {
			return fmt.Errorf("materialize fact: %w", err)
		}
	}
	for _, v := range s.Knowledge {
		if _, err := r.tx.Exec(ctx, `INSERT INTO knowledge_entries(timeline_id,fact_id,knower_character_id,confidence,learned_at_event_seq,invalidated_at_event_seq) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(timeline_id,fact_id,knower_character_id) DO UPDATE SET confidence=EXCLUDED.confidence,learned_at_event_seq=EXCLUDED.learned_at_event_seq,invalidated_at_event_seq=EXCLUDED.invalidated_at_event_seq`, v.TimelineID, v.FactID, v.KnowerCharacterID, v.Confidence, v.LearnedAtEventSeq, v.InvalidatedAtEventSeq); err != nil {
			return fmt.Errorf("materialize knowledge: %w", err)
		}
	}
	for _, v := range s.Beliefs {
		if _, err := r.tx.Exec(ctx, `INSERT INTO character_beliefs(id,timeline_id,character_id,subject_type,subject_id,predicate,object,stance,confidence,status,source_event_seq) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(id) DO UPDATE SET character_id=EXCLUDED.character_id,subject_type=EXCLUDED.subject_type,subject_id=EXCLUDED.subject_id,predicate=EXCLUDED.predicate,object=EXCLUDED.object,stance=EXCLUDED.stance,confidence=EXCLUDED.confidence,status=EXCLUDED.status,source_event_seq=EXCLUDED.source_event_seq`, v.ID, v.TimelineID, v.CharacterID, v.SubjectType, nilID(v.SubjectID), v.Predicate, jsonOr(v.Object, "null"), v.Stance, v.Confidence, v.Status, v.SourceEventSeq); err != nil {
			return fmt.Errorf("materialize belief: %w", err)
		}
	}
	for _, v := range s.WorldSystems {
		if _, err := r.tx.Exec(ctx, `INSERT INTO world_systems(timeline_id,system_id,name,kind,description,resources,status,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(timeline_id,system_id) DO UPDATE SET name=EXCLUDED.name,kind=EXCLUDED.kind,description=EXCLUDED.description,resources=EXCLUDED.resources,status=EXCLUDED.status,version=EXCLUDED.version,updated_at=now()`, v.TimelineID, v.ID, v.Name, v.Kind, v.Description, jsonOr(v.Resources, "[]"), v.Status, v.Version); err != nil {
			return fmt.Errorf("materialize world system: %w", err)
		}
	}
	for _, v := range s.WorldRules {
		if _, err := r.tx.Exec(ctx, `INSERT INTO world_rules(timeline_id,rule_id,system_id,title,category,severity,statement,preconditions,costs,forbidden_results,exceptions,tags,visibility,status,exception_of,source,evidence,version,established_at_seq) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) ON CONFLICT(timeline_id,rule_id) DO UPDATE SET system_id=EXCLUDED.system_id,title=EXCLUDED.title,category=EXCLUDED.category,severity=EXCLUDED.severity,statement=EXCLUDED.statement,preconditions=EXCLUDED.preconditions,costs=EXCLUDED.costs,forbidden_results=EXCLUDED.forbidden_results,exceptions=EXCLUDED.exceptions,tags=EXCLUDED.tags,visibility=EXCLUDED.visibility,status=EXCLUDED.status,exception_of=EXCLUDED.exception_of,source=EXCLUDED.source,evidence=EXCLUDED.evidence,version=EXCLUDED.version,updated_at=now()`, v.TimelineID, v.ID, v.SystemID, v.Title, v.Category, v.Severity, v.Statement, jsonOr(v.Preconditions, "[]"), jsonOr(v.Costs, "[]"), jsonOr(v.ForbiddenResults, "[]"), jsonOr(v.Exceptions, "[]"), jsonOr(v.Tags, "[]"), v.Visibility, v.Status, v.ExceptionOf, v.Source, v.Evidence, v.Version, v.EstablishedAtSeq); err != nil {
			return fmt.Errorf("materialize world rule: %w", err)
		}
	}
	for _, v := range s.WorldResources {
		if _, err := r.tx.Exec(ctx, `INSERT INTO world_resource_states(timeline_id,resource_id,system_id,owner_type,owner_id,owner_key,name,unit,current_value,min_value,max_value,visibility,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(timeline_id,resource_id,owner_type,owner_key) DO UPDATE SET system_id=EXCLUDED.system_id,owner_id=EXCLUDED.owner_id,name=EXCLUDED.name,unit=EXCLUDED.unit,current_value=EXCLUDED.current_value,min_value=EXCLUDED.min_value,max_value=EXCLUDED.max_value,visibility=EXCLUDED.visibility,version=EXCLUDED.version,updated_at=now()`, v.TimelineID, v.ResourceID, v.SystemID, v.OwnerType, nilID(v.OwnerID), v.OwnerKey, v.Name, v.Unit, v.Current, v.Minimum, v.Maximum, v.Visibility, v.Version); err != nil {
			return fmt.Errorf("materialize world resource: %w", err)
		}
	}
	for _, v := range s.Items {
		if _, err := r.tx.Exec(ctx, `INSERT INTO item_states(timeline_id,item_id,owner_character_id,location_id,condition,state,version) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(timeline_id,item_id) DO UPDATE SET owner_character_id=EXCLUDED.owner_character_id,location_id=EXCLUDED.location_id,condition=EXCLUDED.condition,state=EXCLUDED.state,version=EXCLUDED.version,updated_at=now()`, v.TimelineID, v.ItemID, nilID(v.OwnerCharacterID), nilID(v.LocationID), v.Condition, jsonOr(v.State, "{}"), v.Version); err != nil {
			return fmt.Errorf("materialize item: %w", err)
		}
	}
	for _, v := range s.Threads {
		if _, err := r.tx.Exec(ctx, `INSERT INTO story_threads(id,timeline_id,title,summary,importance,status,metadata,introduced_at_seq,last_touched_at_seq) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(id) DO UPDATE SET title=EXCLUDED.title,summary=EXCLUDED.summary,importance=EXCLUDED.importance,status=EXCLUDED.status,metadata=EXCLUDED.metadata,last_touched_at_seq=EXCLUDED.last_touched_at_seq,updated_at=now()`, v.ID, v.TimelineID, v.Title, v.Summary, v.Importance, v.Status, jsonOr(v.Metadata, "{}"), v.IntroducedAtSeq, v.LastTouchedAtSeq); err != nil {
			return fmt.Errorf("materialize thread: %w", err)
		}
	}
	for _, v := range s.DirectorInstructions {
		if _, err := r.tx.Exec(ctx, `INSERT INTO director_instructions(id,timeline_id,instruction_text,scope,priority,status,created_at_seq,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET instruction_text=EXCLUDED.instruction_text,scope=EXCLUDED.scope,priority=EXCLUDED.priority,status=EXCLUDED.status,expires_at=EXCLUDED.expires_at`, v.ID, v.TimelineID, v.Text, v.Scope, v.Priority, v.Status, v.CreatedAtSeq, nullableJSON(v.ExpiresAt)); err != nil {
			return fmt.Errorf("materialize director instruction: %w", err)
		}
	}
	return nil
}
