package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/local/interactive-ai-story/backend/internal/domain/director"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/directorrepo"
)

type DirectorRepository struct{ pool *pgxpool.Pool }

func NewDirectorRepository(p *pgxpool.Pool) *DirectorRepository { return &DirectorRepository{pool: p} }

func (r *DirectorRepository) View(ctx context.Context, tid timeline.ID) (directorrepo.View, error) {
	v := directorrepo.View{
		Characters: []map[string]any{}, Relationships: []map[string]any{}, Stats: []map[string]any{},
		Locations: []map[string]any{},
		Items:     []map[string]any{}, Facts: []map[string]any{}, Knowledge: []map[string]any{},
		Beliefs: []map[string]any{}, Threads: []map[string]any{}, Objectives: []map[string]any{}, Abilities: []map[string]any{}, Attributes: []map[string]any{}, Inventory: []map[string]any{},
		WorldSystems: []map[string]any{}, WorldRules: []map[string]any{}, WorldResources: []map[string]any{}, WorldRuleAudit: []map[string]any{},
		Instructions: []directorrepo.InstructionView{},
	}
	if e := r.pool.QueryRow(ctx, `SELECT id,semantic_revision,head_event_seq FROM timelines WHERE id=$1`, tid).Scan(&v.TimelineID, &v.SemanticRevision, &v.HeadEventSeq); e != nil {
		return v, e
	}
	// This Phase-9 view intentionally returns typed JSON rows rather than exposing raw DB JSON blobs as an editing contract.
	queries := []struct {
		dst *[]map[string]any
		q   string
	}{
		{&v.Characters, `SELECT jsonb_build_object(
          'id',c.id,'name',COALESCE(NULLIF(cs.director_profile->>'name',''),c.name),
          'kind',c.kind,'age',COALESCE((cs.director_profile->>'age')::int,c.adult_age),
          'role',COALESCE(cs.director_profile->>'role',c.core->>'role',''),
          'personality',COALESCE(cs.director_profile->>'personality',c.core->>'personality',c.core->>'character',''),
          'relationship',COALESCE(cs.director_profile->>'relationship',c.core->>'relationship',c.core->>'relationshipToHero',''),
          'visualAnchorEn',COALESCE(cs.director_profile->>'visualAnchorEn',c.visual_profile->>'anchorEn',c.core->>'visualAnchorEn',''),
          'mood',cs.mood,'goal',cs.current_goal,'active',cs.active)
        FROM characters c JOIN character_states cs ON cs.character_id=c.id
        WHERE cs.timeline_id=$1
        ORDER BY cs.active DESC,COALESCE(NULLIF(cs.director_profile->>'name',''),c.name)`},
		{&v.Locations, `SELECT jsonb_build_object(
          'id',l.id,'name',COALESCE(NULLIF(ls.name_override,''),l.name),
          'description',COALESCE(NULLIF(ls.description_override,''),l.description),
          'visualAnchorEn',COALESCE(NULLIF(ls.visual_profile_override->>'anchorEn',''),l.visual_profile->>'anchorEn',''),
          'active',ls.active)
        FROM locations l JOIN location_states ls ON ls.location_id=l.id
        WHERE ls.timeline_id=$1
        ORDER BY ls.active DESC,COALESCE(NULLIF(ls.name_override,''),l.name)`},
		{&v.Relationships, `SELECT jsonb_build_object('id',id,'fromCharacterId',from_character_id,'toCharacterId',to_character_id,'status',status,'summary',summary) FROM relationships WHERE timeline_id=$1 ORDER BY created_at`},
		{&v.Stats, `SELECT jsonb_build_object('ownerType',owner_type,'ownerId',owner_id,'key',key,'valueType',value_type,'value',value,'targetValue',target_value,'evolutionMode',evolution_mode) FROM stats WHERE timeline_id=$1 ORDER BY owner_type,key`},
		{&v.Items, `SELECT jsonb_build_object('itemId',s.item_id,'name',i.name,'ownerCharacterId',s.owner_character_id,'locationId',s.location_id,'condition',s.condition) FROM item_states s JOIN items i ON i.id=s.item_id WHERE s.timeline_id=$1 ORDER BY i.name`},
		{&v.Facts, `SELECT jsonb_build_object('id',id,'subjectType',subject_type,'subjectId',subject_id,'predicate',predicate,'object',object,'status',status) FROM facts WHERE timeline_id=$1 ORDER BY created_at DESC`},
		{&v.Knowledge, `SELECT jsonb_build_object('factId',fact_id,'knowerCharacterId',knower_character_id,'confidence',confidence,'invalidatedAtEventSeq',invalidated_at_event_seq) FROM knowledge_entries WHERE timeline_id=$1`},
		{&v.Beliefs, `SELECT jsonb_build_object('id',id,'characterId',character_id,'subjectType',subject_type,'subjectId',subject_id,'predicate',predicate,'object',object,'stance',stance,'confidence',confidence,'status',status) FROM character_beliefs WHERE timeline_id=$1 ORDER BY created_at DESC`},
		{&v.Threads, `SELECT jsonb_build_object('id',id,'title',title,'summary',summary,'importance',importance,'status',status) FROM story_threads WHERE timeline_id=$1 ORDER BY importance DESC`},
		{&v.Objectives, `SELECT jsonb_build_object(
          'id',id,'scope',metadata->>'scope','kind',COALESCE(metadata->>'objectiveKind',CASE metadata->>'scope' WHEN 'global' THEN 'quest' ELSE 'task' END),
          'questType',COALESCE(metadata->>'questType','main'),
          'parentObjectiveId',NULLIF(metadata->>'parentObjectiveId',''),'title',title,'description',summary,
          'successCriteria',metadata->>'successCriteria','progress',COALESCE((metadata->>'progress')::int,0),
          'evidence',COALESCE(metadata->>'evidence',''),'status',CASE status WHEN 'closed' THEN 'completed' WHEN 'abandoned' THEN 'failed' ELSE 'active' END)
        FROM story_threads WHERE timeline_id=$1 AND metadata->>'kind'='objective'
        ORDER BY CASE metadata->>'scope' WHEN 'global' THEN 0 ELSE 1 END,importance DESC,last_touched_at_seq DESC`},
		{&v.Abilities, `SELECT jsonb_build_object('id',id,'category','ability','name',title,'description',summary,'level',metadata->>'level','status',CASE status WHEN 'open' THEN 'active' ELSE 'inactive' END,'evidence',COALESCE(metadata->>'evidence',''),'tags',COALESCE(metadata->'tags','[]'::jsonb)) FROM story_threads WHERE timeline_id=$1 AND metadata->>'kind'='hero_journal' AND metadata->>'category'='ability' ORDER BY CASE status WHEN 'open' THEN 0 ELSE 1 END,last_touched_at_seq DESC`},
		{&v.Attributes, `SELECT jsonb_build_object('id',id,'category','attribute','name',title,'description',summary,'level',metadata->>'level','status',CASE status WHEN 'open' THEN 'active' ELSE 'inactive' END,'evidence',COALESCE(metadata->>'evidence',''),'tags',COALESCE(metadata->'tags','[]'::jsonb)) FROM story_threads WHERE timeline_id=$1 AND metadata->>'kind'='hero_journal' AND metadata->>'category'='attribute' ORDER BY CASE status WHEN 'open' THEN 0 ELSE 1 END,last_touched_at_seq DESC`},
		{&v.Inventory, `SELECT jsonb_build_object('id',id,'category',metadata->>'category','name',title,'description',summary,'quantity',COALESCE((metadata->>'quantity')::int,0),'status',CASE status WHEN 'open' THEN 'active' ELSE 'inactive' END,'evidence',COALESCE(metadata->>'evidence',''),'tags',COALESCE(metadata->'tags','[]'::jsonb)) FROM story_threads WHERE timeline_id=$1 AND metadata->>'kind'='hero_journal' AND metadata->>'category' IN ('item','currency') ORDER BY CASE metadata->>'category' WHEN 'currency' THEN 0 ELSE 1 END,CASE status WHEN 'open' THEN 0 ELSE 1 END,last_touched_at_seq DESC`},
		{&v.WorldSystems, `SELECT jsonb_build_object('systemId',system_id,'name',name,'kind',kind,'description',description,'resources',resources,'status',status,'version',version) FROM world_systems WHERE timeline_id=$1 ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'pending' THEN 1 ELSE 2 END,name`},
		{&v.WorldRules, `SELECT jsonb_build_object('ruleId',rule_id,'systemId',system_id,'title',title,'category',category,'severity',severity,'statement',statement,'preconditions',preconditions,'costs',costs,'forbiddenResults',forbidden_results,'exceptions',exceptions,'tags',tags,'visibility',visibility,'status',status,'exceptionOf',exception_of,'source',source,'evidence',evidence,'version',version,'establishedAtSeq',established_at_seq) FROM world_rules WHERE timeline_id=$1 ORDER BY CASE status WHEN 'pending' THEN 0 WHEN 'established' THEN 1 WHEN 'superseded' THEN 2 ELSE 3 END,CASE severity WHEN 'hard' THEN 0 WHEN 'mystery' THEN 1 ELSE 2 END,rule_id`},
		{&v.WorldResources, `SELECT jsonb_build_object('resourceId',resource_id,'systemId',system_id,'ownerType',owner_type,'ownerId',owner_id,'ownerKey',owner_key,'name',name,'unit',unit,'currentValue',current_value::float8,'minValue',min_value::float8,'maxValue',max_value::float8,'visibility',visibility,'version',version) FROM world_resource_states WHERE timeline_id=$1 ORDER BY system_id,name,owner_type,owner_key`},
		{&v.WorldRuleAudit, `SELECT jsonb_build_object('id',id,'generationId',generation_id,'beatId',beat_id,'ruleId',rule_id,'severity',severity,'evidence',evidence,'repairInstruction',repair_instruction,'status',status,'createdAt',created_at) FROM world_rule_audit WHERE timeline_id=$1 ORDER BY created_at DESC LIMIT 50`},
	}
	for _, x := range queries {
		rows, e := r.pool.Query(ctx, x.q, tid)
		if e != nil {
			return v, e
		}
		for rows.Next() {
			var raw []byte
			if e = rows.Scan(&raw); e != nil {
				rows.Close()
				return v, e
			}
			var m map[string]any
			if e = json.Unmarshal(raw, &m); e != nil {
				rows.Close()
				return v, e
			}
			*x.dst = append(*x.dst, m)
		}
		rows.Close()
	}
	if len(v.Objectives) == 0 {
		var chapterGoal, sceneGoal string
		if err := r.pool.QueryRow(ctx, `SELECT c.goal,s.goal FROM chapters c JOIN scenes s ON s.chapter_id=c.id WHERE c.timeline_id=$1 AND c.status IN('active','completing') AND s.status IN('active','awaiting_player','completing') ORDER BY c.number DESC,s.number DESC LIMIT 1`, tid).Scan(&chapterGoal, &sceneGoal); err == nil {
			if chapterGoal != "" {
				v.Objectives = append(v.Objectives, map[string]any{"id": "virtual-global", "scope": "global", "kind": "quest", "questType": "main", "title": chapterGoal, "successCriteria": chapterGoal, "status": "active", "progress": 0})
			}
			if sceneGoal != "" && sceneGoal != chapterGoal {
				v.Objectives = append(v.Objectives, map[string]any{"id": "virtual-minor", "parentObjectiveId": "virtual-global", "scope": "minor", "kind": "task", "questType": "main", "title": sceneGoal, "successCriteria": sceneGoal, "status": "active", "progress": 0})
			}
		}
	}
	if _, e := r.ActiveInstructions(ctx, tid); e != nil {
		return v, e
	}
	ins, e := r.listInstructions(ctx, tid, false)
	if e != nil {
		return v, e
	}
	for _, instruction := range ins {
		v.Instructions = append(v.Instructions, directorrepo.InstructionView{
			ID: instruction.ID, TimelineID: instruction.TimelineID, Text: instruction.Text,
			Scope: instruction.Scope, Priority: instruction.Priority, Status: instruction.Status,
			CreatedAtSeq: instruction.CreatedAtSeq, ExpiresAt: instruction.ExpiresAt,
		})
	}
	return v, nil
}

func (r *DirectorRepository) ApplyExact(ctx context.Context, c director.ExactCommand) (director.AuditEntry, error) {
	if e := c.Validate(); e != nil {
		return director.AuditEntry{}, e
	}
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return director.AuditEntry{}, e
	}
	defer tx.Rollback(ctx)
	var rev int64
	if e = tx.QueryRow(ctx, `SELECT semantic_revision FROM timelines WHERE id=$1 FOR UPDATE`, c.TimelineID).Scan(&rev); e != nil {
		return director.AuditEntry{}, e
	}
	if rev != c.ExpectedRevision {
		return director.AuditEntry{}, director.ErrRevisionMoved
	}
	before, after, targetType, e := applyDirectorCommand(ctx, tx, c)
	if e != nil {
		return director.AuditEntry{}, e
	}
	var head int64
	if e = tx.QueryRow(ctx, `SELECT head_event_seq FROM timelines WHERE id=$1`, c.TimelineID).Scan(&head); e != nil {
		return director.AuditEntry{}, e
	}
	eid, e := id.New()
	if e != nil {
		return director.AuditEntry{}, e
	}
	eventPayload, _ := json.Marshal(map[string]any{"commandType": c.Type, "targetType": targetType, "targetId": c.TargetID, "after": json.RawMessage(after)})
	if _, e = tx.Exec(ctx, `INSERT INTO story_events(id,timeline_id,seq,event_type,schema_version,payload,created_at) VALUES($1,$2,$3,'director_exact_edit_applied',1,$4,now())`, eid, c.TimelineID, head+1, eventPayload); e != nil {
		return director.AuditEntry{}, e
	}
	aid, e := id.New()
	if e != nil {
		return director.AuditEntry{}, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO director_audit_log(id,timeline_id,command_type,target_type,target_id,before_state,after_state,note) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, aid, c.TimelineID, string(c.Type), targetType, nullID(c.TargetID), before, after, c.Note); e != nil {
		return director.AuditEntry{}, e
	}
	if _, e = tx.Exec(ctx, `UPDATE timelines SET semantic_revision=semantic_revision+1,head_event_seq=$2,updated_at=now() WHERE id=$1`, c.TimelineID, head+1); e != nil {
		return director.AuditEntry{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return director.AuditEntry{}, e
	}
	return director.AuditEntry{ID: aid, TimelineID: c.TimelineID, CommandType: string(c.Type), TargetType: targetType, TargetID: c.TargetID, Before: before, After: after, Note: c.Note}, nil
}

func applyDirectorCommand(ctx context.Context, tx pgx.Tx, c director.ExactCommand) (json.RawMessage, json.RawMessage, string, error) {
	switch c.Type {
	case director.SetStat:
		var p struct {
			OwnerType, OwnerID, Key, ValueType string
			Value                              json.RawMessage
			TargetValue                        json.RawMessage
			EvolutionMode                      string
		}
		if json.Unmarshal(c.Payload, &p) != nil || p.OwnerType == "" || p.OwnerID == "" || p.Key == "" || !json.Valid(p.Value) {
			return nil, nil, "stat", director.ErrInvalidCommand
		}
		var before []byte
		_ = tx.QueryRow(ctx, `SELECT to_jsonb(s) FROM stats s WHERE timeline_id=$1 AND owner_type=$2 AND owner_id=$3 AND key=$4`, c.TimelineID, p.OwnerType, p.OwnerID, p.Key).Scan(&before)
		_, e := tx.Exec(ctx, `INSERT INTO stats(timeline_id,owner_type,owner_id,key,value_type,value,target_value,evolution_mode) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(timeline_id,owner_type,owner_id,key) DO UPDATE SET value_type=EXCLUDED.value_type,value=EXCLUDED.value,target_value=EXCLUDED.target_value,evolution_mode=EXCLUDED.evolution_mode,updated_at=now()`, c.TimelineID, p.OwnerType, p.OwnerID, p.Key, p.ValueType, p.Value, p.TargetValue, p.EvolutionMode)
		if e != nil {
			return nil, nil, "stat", e
		}
		after, _ := json.Marshal(p)
		return before, after, "stat", nil
	case director.TransferItem:
		var p struct {
			ItemID, OwnerCharacterID, LocationID string
			Condition                            string
		}
		if json.Unmarshal(c.Payload, &p) != nil || p.ItemID == "" || (p.OwnerCharacterID != "" && p.LocationID != "") {
			return nil, nil, "item", director.ErrInvalidCommand
		}
		var before []byte
		if e := tx.QueryRow(ctx, `SELECT to_jsonb(s) FROM item_states s WHERE timeline_id=$1 AND item_id=$2`, c.TimelineID, p.ItemID).Scan(&before); e != nil {
			return nil, nil, "item", e
		}
		_, e := tx.Exec(ctx, `UPDATE item_states SET owner_character_id=NULLIF($3,'')::uuid,location_id=NULLIF($4,'')::uuid,condition=$5,version=version+1,updated_at=now() WHERE timeline_id=$1 AND item_id=$2`, c.TimelineID, p.ItemID, p.OwnerCharacterID, p.LocationID, p.Condition)
		if e != nil {
			return nil, nil, "item", e
		}
		after, _ := json.Marshal(p)
		return before, after, "item", nil
	case director.UpsertFact:
		var p struct {
			ID, SubjectType, SubjectID, Predicate string
			Object                                json.RawMessage
		}
		if json.Unmarshal(c.Payload, &p) != nil || p.SubjectType == "" || p.SubjectID == "" || p.Predicate == "" || !json.Valid(p.Object) {
			return nil, nil, "fact", director.ErrInvalidCommand
		}
		fid := p.ID
		if fid == "" {
			x, _ := id.New()
			fid = x.String()
		}
		var before []byte
		_ = tx.QueryRow(ctx, `SELECT to_jsonb(f) FROM facts f WHERE id=$1 AND timeline_id=$2`, fid, c.TimelineID).Scan(&before)
		var head int64
		if e := tx.QueryRow(ctx, `SELECT head_event_seq FROM timelines WHERE id=$1`, c.TimelineID).Scan(&head); e != nil {
			return nil, nil, "fact", e
		}
		if head < 1 {
			head = 1
		}
		_, e := tx.Exec(ctx, `INSERT INTO facts(id,timeline_id,subject_type,subject_id,predicate,object,status,valid_from_seq) VALUES($1,$2,$3,$4,$5,$6,'active',$7) ON CONFLICT(id) DO UPDATE SET subject_type=EXCLUDED.subject_type,subject_id=EXCLUDED.subject_id,predicate=EXCLUDED.predicate,object=EXCLUDED.object,status='active',invalidated_at_seq=NULL`, fid, c.TimelineID, p.SubjectType, p.SubjectID, p.Predicate, p.Object, head)
		if e != nil {
			return nil, nil, "fact", e
		}
		p.ID = fid
		after, _ := json.Marshal(p)
		return before, after, "fact", nil
	case director.GrantKnowledge:
		var p struct {
			FactID, CharacterID string
			Confidence          float64
		}
		if json.Unmarshal(c.Payload, &p) != nil || p.FactID == "" || p.CharacterID == "" || p.Confidence < 0 || p.Confidence > 1 {
			return nil, nil, "knowledge", director.ErrInvalidCommand
		}
		var before []byte
		_ = tx.QueryRow(ctx, `SELECT to_jsonb(k) FROM knowledge_entries k WHERE timeline_id=$1 AND fact_id=$2 AND knower_character_id=$3`, c.TimelineID, p.FactID, p.CharacterID).Scan(&before)
		var head int64
		_ = tx.QueryRow(ctx, `SELECT GREATEST(head_event_seq,1) FROM timelines WHERE id=$1`, c.TimelineID).Scan(&head)
		_, e := tx.Exec(ctx, `INSERT INTO knowledge_entries(timeline_id,fact_id,knower_character_id,confidence,learned_at_event_seq) VALUES($1,$2,$3,$4,$5) ON CONFLICT(timeline_id,fact_id,knower_character_id) DO UPDATE SET confidence=EXCLUDED.confidence,invalidated_at_event_seq=NULL`, c.TimelineID, p.FactID, p.CharacterID, p.Confidence, head)
		if e != nil {
			return nil, nil, "knowledge", e
		}
		after, _ := json.Marshal(p)
		return before, after, "knowledge", nil
	case director.UpsertBelief:
		var p struct {
			ID, CharacterID, SubjectType, SubjectID, Predicate, Stance string
			Object                                                     json.RawMessage
			Confidence                                                 float64
		}
		if json.Unmarshal(c.Payload, &p) != nil || p.CharacterID == "" || p.Predicate == "" || !json.Valid(p.Object) || p.Confidence < 0 || p.Confidence > 1 {
			return nil, nil, "belief", director.ErrInvalidCommand
		}
		bid := p.ID
		if bid == "" {
			x, _ := id.New()
			bid = x.String()
		}
		var before []byte
		_ = tx.QueryRow(ctx, `SELECT to_jsonb(b) FROM character_beliefs b WHERE id=$1 AND timeline_id=$2`, bid, c.TimelineID).Scan(&before)
		_, e := tx.Exec(ctx, `INSERT INTO character_beliefs(id,timeline_id,character_id,subject_type,subject_id,predicate,object,stance,confidence,status) VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,$6,$7,$8,$9,'active') ON CONFLICT(id) DO UPDATE SET character_id=EXCLUDED.character_id,subject_type=EXCLUDED.subject_type,subject_id=EXCLUDED.subject_id,predicate=EXCLUDED.predicate,object=EXCLUDED.object,stance=EXCLUDED.stance,confidence=EXCLUDED.confidence,status='active'`, bid, c.TimelineID, p.CharacterID, p.SubjectType, p.SubjectID, p.Predicate, p.Object, p.Stance, p.Confidence)
		if e != nil {
			return nil, nil, "belief", e
		}
		p.ID = bid
		after, _ := json.Marshal(p)
		return before, after, "belief", nil
	case director.UpdateCharacterState:
		var p struct {
			CharacterID, Mood, CurrentGoal string
			Active                         bool
		}
		if json.Unmarshal(c.Payload, &p) != nil || p.CharacterID == "" {
			return nil, nil, "character_state", director.ErrInvalidCommand
		}
		var before []byte
		if e := tx.QueryRow(ctx, `SELECT to_jsonb(s) FROM character_states s WHERE timeline_id=$1 AND character_id=$2`, c.TimelineID, p.CharacterID).Scan(&before); e != nil {
			return nil, nil, "character_state", e
		}
		_, e := tx.Exec(ctx, `UPDATE character_states SET mood=$3,current_goal=$4,active=$5,version=version+1,updated_at=now() WHERE timeline_id=$1 AND character_id=$2`, c.TimelineID, p.CharacterID, p.Mood, p.CurrentGoal, p.Active)
		if e != nil {
			return nil, nil, "character_state", e
		}
		after, _ := json.Marshal(p)
		return before, after, "character_state", nil
	case director.UpdateRelationship:
		var p struct{ RelationshipID, Status, Summary string }
		if json.Unmarshal(c.Payload, &p) != nil || p.RelationshipID == "" {
			return nil, nil, "relationship", director.ErrInvalidCommand
		}
		var before []byte
		if e := tx.QueryRow(ctx, `SELECT to_jsonb(r) FROM relationships r WHERE id=$1 AND timeline_id=$2`, p.RelationshipID, c.TimelineID).Scan(&before); e != nil {
			return nil, nil, "relationship", e
		}
		_, e := tx.Exec(ctx, `UPDATE relationships SET status=$3,summary=$4,updated_at=now() WHERE id=$1 AND timeline_id=$2`, p.RelationshipID, c.TimelineID, p.Status, p.Summary)
		if e != nil {
			return nil, nil, "relationship", e
		}
		after, _ := json.Marshal(p)
		return before, after, "relationship", nil
	case director.UpdateThread:
		var p struct {
			ThreadID, Title, Summary, Status string
			Importance                       float64
		}
		if json.Unmarshal(c.Payload, &p) != nil || p.ThreadID == "" || p.Importance < 0 || p.Importance > 1 {
			return nil, nil, "thread", director.ErrInvalidCommand
		}
		var before []byte
		if e := tx.QueryRow(ctx, `SELECT to_jsonb(t) FROM story_threads t WHERE id=$1 AND timeline_id=$2`, p.ThreadID, c.TimelineID).Scan(&before); e != nil {
			return nil, nil, "thread", e
		}
		_, e := tx.Exec(ctx, `UPDATE story_threads SET title=$3,summary=$4,status=$5,importance=$6,updated_at=now() WHERE id=$1 AND timeline_id=$2`, p.ThreadID, c.TimelineID, p.Title, p.Summary, p.Status, p.Importance)
		if e != nil {
			return nil, nil, "thread", e
		}
		after, _ := json.Marshal(p)
		return before, after, "thread", nil
	case director.UpsertCharacter:
		var p struct {
			CharacterID, Name, Kind, Role, Personality, Relationship, VisualAnchorEn string
			Mood, CurrentGoal                                                        string
			Age                                                                      int
			Active                                                                   *bool
		}
		if json.Unmarshal(c.Payload, &p) != nil || p.Name == "" || p.Age < 1 || p.Age > 150 {
			return nil, nil, "character", director.ErrInvalidCommand
		}
		var storyID string
		if e := tx.QueryRow(ctx, `SELECT story_id::text FROM timelines WHERE id=$1`, c.TimelineID).Scan(&storyID); e != nil {
			return nil, nil, "character", e
		}
		active := true
		if p.Active != nil {
			active = *p.Active
		}
		if p.Kind == "" {
			p.Kind = "persistent_npc"
		}
		if p.Kind != "player" && p.Kind != "persistent_npc" && p.Kind != "temporary_promoted" {
			return nil, nil, "character", director.ErrInvalidCommand
		}
		profile, _ := json.Marshal(map[string]any{"name": p.Name, "age": p.Age, "role": p.Role, "personality": p.Personality, "relationship": p.Relationship, "visualAnchorEn": p.VisualAnchorEn})
		var before []byte
		var characterExists bool
		if p.CharacterID != "" {
			_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM character_states WHERE timeline_id=$1 AND character_id=$2)`, c.TimelineID, p.CharacterID).Scan(&characterExists)
		}
		if p.CharacterID == "" || !characterExists {
			fresh, err := id.New()
			if err != nil {
				return nil, nil, "character", err
			}
			if p.CharacterID == "" {
				p.CharacterID = fresh.String()
			}
			core, _ := json.Marshal(map[string]any{"role": p.Role, "personality": p.Personality, "relationship": p.Relationship, "visualAnchorEn": p.VisualAnchorEn})
			visual, _ := json.Marshal(map[string]any{"anchorEn": p.VisualAnchorEn})
			if _, e := tx.Exec(ctx, `INSERT INTO characters(id,story_id,kind,name,adult_age,core,visual_profile) VALUES($1,$2,$3,$4,$5,$6,$7)`, p.CharacterID, storyID, p.Kind, p.Name, p.Age, core, visual); e != nil {
				return nil, nil, "character", e
			}
			if _, e := tx.Exec(ctx, `INSERT INTO character_states(timeline_id,character_id,mood,current_goal,active,current_appearance,director_profile,version) VALUES($1,$2,$3,$4,$5,'{}',$6,1)`, c.TimelineID, p.CharacterID, p.Mood, p.CurrentGoal, active, profile); e != nil {
				return nil, nil, "character", e
			}
		} else {
			var storedKind string
			if e := tx.QueryRow(ctx, `SELECT c.kind,to_jsonb(cs) FROM characters c JOIN character_states cs ON cs.character_id=c.id WHERE c.id=$1 AND c.story_id=$2 AND cs.timeline_id=$3`, p.CharacterID, storyID, c.TimelineID).Scan(&storedKind, &before); e != nil {
				return nil, nil, "character", e
			}
			p.Kind = storedKind
			if _, e := tx.Exec(ctx, `UPDATE character_states SET mood=$3,current_goal=$4,active=$5,director_profile=$6,version=version+1,updated_at=now() WHERE timeline_id=$1 AND character_id=$2`, c.TimelineID, p.CharacterID, p.Mood, p.CurrentGoal, active, profile); e != nil {
				return nil, nil, "character", e
			}
		}
		p.Active = &active
		after, _ := json.Marshal(p)
		return before, after, "character", nil
	case director.ArchiveCharacter:
		var p struct{ CharacterID string }
		if json.Unmarshal(c.Payload, &p) != nil || p.CharacterID == "" {
			return nil, nil, "character", director.ErrInvalidCommand
		}
		var before []byte
		var kind string
		if e := tx.QueryRow(ctx, `SELECT c.kind,to_jsonb(cs) FROM characters c JOIN character_states cs ON cs.character_id=c.id WHERE cs.timeline_id=$1 AND c.id=$2`, c.TimelineID, p.CharacterID).Scan(&kind, &before); e != nil {
			return nil, nil, "character", e
		}
		if kind == "player" {
			return nil, nil, "character", director.ErrInvalidCommand
		}
		if _, e := tx.Exec(ctx, `UPDATE character_states SET active=false,version=version+1,updated_at=now() WHERE timeline_id=$1 AND character_id=$2`, c.TimelineID, p.CharacterID); e != nil {
			return nil, nil, "character", e
		}
		after, _ := json.Marshal(p)
		return before, after, "character", nil
	case director.UpsertLocation:
		var p struct {
			LocationID, Name, Description, VisualAnchorEn string
			Active                                        *bool
		}
		if json.Unmarshal(c.Payload, &p) != nil || p.Name == "" {
			return nil, nil, "location", director.ErrInvalidCommand
		}
		var storyID string
		if e := tx.QueryRow(ctx, `SELECT story_id::text FROM timelines WHERE id=$1`, c.TimelineID).Scan(&storyID); e != nil {
			return nil, nil, "location", e
		}
		active := true
		if p.Active != nil {
			active = *p.Active
		}
		visual, _ := json.Marshal(map[string]any{"anchorEn": p.VisualAnchorEn})
		var before []byte
		var locationExists bool
		if p.LocationID != "" {
			_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM location_states WHERE timeline_id=$1 AND location_id=$2)`, c.TimelineID, p.LocationID).Scan(&locationExists)
		}
		if p.LocationID == "" || !locationExists {
			fresh, err := id.New()
			if err != nil {
				return nil, nil, "location", err
			}
			if p.LocationID == "" {
				p.LocationID = fresh.String()
			}
			if _, e := tx.Exec(ctx, `INSERT INTO locations(id,story_id,name,description,visual_profile) VALUES($1,$2,$3,$4,$5)`, p.LocationID, storyID, p.Name, p.Description, visual); e != nil {
				return nil, nil, "location", e
			}
			if _, e := tx.Exec(ctx, `INSERT INTO location_states(timeline_id,location_id,name_override,description_override,visual_profile_override,active,version) VALUES($1,$2,$3,$4,$5,$6,1)`, c.TimelineID, p.LocationID, p.Name, p.Description, visual, active); e != nil {
				return nil, nil, "location", e
			}
		} else {
			if e := tx.QueryRow(ctx, `SELECT to_jsonb(ls) FROM location_states ls JOIN locations l ON l.id=ls.location_id JOIN timelines t ON t.id=ls.timeline_id WHERE ls.timeline_id=$1 AND ls.location_id=$2 AND l.story_id=t.story_id`, c.TimelineID, p.LocationID).Scan(&before); e != nil {
				return nil, nil, "location", e
			}
			if _, e := tx.Exec(ctx, `UPDATE location_states SET name_override=$3,description_override=$4,visual_profile_override=$5,active=$6,version=version+1,updated_at=now() WHERE timeline_id=$1 AND location_id=$2`, c.TimelineID, p.LocationID, p.Name, p.Description, visual, active); e != nil {
				return nil, nil, "location", e
			}
		}
		p.Active = &active
		after, _ := json.Marshal(p)
		return before, after, "location", nil
	case director.ArchiveLocation:
		var p struct{ LocationID string }
		if json.Unmarshal(c.Payload, &p) != nil || p.LocationID == "" {
			return nil, nil, "location", director.ErrInvalidCommand
		}
		var before []byte
		if e := tx.QueryRow(ctx, `SELECT to_jsonb(ls) FROM location_states ls WHERE timeline_id=$1 AND location_id=$2`, c.TimelineID, p.LocationID).Scan(&before); e != nil {
			return nil, nil, "location", e
		}
		if _, e := tx.Exec(ctx, `UPDATE location_states SET active=false,version=version+1,updated_at=now() WHERE timeline_id=$1 AND location_id=$2`, c.TimelineID, p.LocationID); e != nil {
			return nil, nil, "location", e
		}
		after, _ := json.Marshal(p)
		return before, after, "location", nil
	case director.UpsertObjective:
		var p struct {
			ObjectiveID, ParentObjectiveID, Scope, Kind, QuestType string
			Title, Description, SuccessCriteria, Status, Evidence  string
			Progress                                               int
		}
		if json.Unmarshal(c.Payload, &p) != nil || p.Title == "" || p.Progress < 0 || p.Progress > 100 || (p.Scope != "global" && p.Scope != "minor") {
			return nil, nil, "objective", director.ErrInvalidCommand
		}
		if p.QuestType == "" {
			p.QuestType = "main"
		}
		if p.QuestType != "main" && p.QuestType != "side" {
			return nil, nil, "objective", director.ErrInvalidCommand
		}
		if p.Status == "" {
			p.Status = "active"
		}
		if p.Status != "active" && p.Status != "completed" && p.Status != "failed" {
			return nil, nil, "objective", director.ErrInvalidCommand
		}
		if p.Scope == "global" {
			p.Kind = "quest"
			p.ParentObjectiveID = ""
		} else {
			if p.Kind == "" {
				p.Kind = "task"
			}
			if p.Kind != "task" && p.Kind != "event" && p.Kind != "milestone" || p.ParentObjectiveID == "" {
				return nil, nil, "objective", director.ErrInvalidCommand
			}
			var parentScope string
			if e := tx.QueryRow(ctx, `SELECT metadata->>'scope' FROM story_threads WHERE timeline_id=$1 AND id=$2 AND metadata->>'kind'='objective'`, c.TimelineID, p.ParentObjectiveID).Scan(&parentScope); e != nil || parentScope != "global" {
				return nil, nil, "objective", director.ErrInvalidCommand
			}
		}
		threadStatus := "open"
		if p.Status == "completed" {
			threadStatus = "closed"
		}
		if p.Status == "failed" {
			threadStatus = "abandoned"
		}
		metadata, _ := json.Marshal(map[string]any{"kind": "objective", "scope": p.Scope, "objectiveKind": p.Kind, "questType": p.QuestType, "parentObjectiveId": p.ParentObjectiveID, "description": p.Description, "successCriteria": p.SuccessCriteria, "progress": p.Progress, "evidence": p.Evidence, "source": "director"})
		importance := .6
		if p.Scope == "global" {
			importance = 1
		}
		var before []byte
		var objectiveExists bool
		if p.ObjectiveID != "" {
			_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM story_threads WHERE timeline_id=$1 AND id=$2 AND metadata->>'kind'='objective')`, c.TimelineID, p.ObjectiveID).Scan(&objectiveExists)
		}
		if p.ObjectiveID == "" || !objectiveExists {
			fresh, err := id.New()
			if err != nil {
				return nil, nil, "objective", err
			}
			if p.ObjectiveID == "" {
				p.ObjectiveID = fresh.String()
			}
			var head int64
			if e := tx.QueryRow(ctx, `SELECT head_event_seq FROM timelines WHERE id=$1`, c.TimelineID).Scan(&head); e != nil {
				return nil, nil, "objective", e
			}
			if _, e := tx.Exec(ctx, `INSERT INTO story_threads(id,timeline_id,title,summary,importance,status,metadata,introduced_at_seq,last_touched_at_seq) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8)`, p.ObjectiveID, c.TimelineID, p.Title, p.Description, importance, threadStatus, metadata, head+1); e != nil {
				return nil, nil, "objective", e
			}
		} else {
			if e := tx.QueryRow(ctx, `SELECT to_jsonb(t) FROM story_threads t WHERE timeline_id=$1 AND id=$2 AND metadata->>'kind'='objective'`, c.TimelineID, p.ObjectiveID).Scan(&before); e != nil {
				return nil, nil, "objective", e
			}
			if _, e := tx.Exec(ctx, `UPDATE story_threads SET title=$3,summary=$4,importance=$5,status=$6,metadata=$7,last_touched_at_seq=(SELECT head_event_seq+1 FROM timelines WHERE id=$1),updated_at=now() WHERE timeline_id=$1 AND id=$2`, c.TimelineID, p.ObjectiveID, p.Title, p.Description, importance, threadStatus, metadata); e != nil {
				return nil, nil, "objective", e
			}
		}
		after, _ := json.Marshal(p)
		return before, after, "objective", nil
	case director.ArchiveObjective:
		var p struct{ ObjectiveID string }
		if json.Unmarshal(c.Payload, &p) != nil || p.ObjectiveID == "" {
			return nil, nil, "objective", director.ErrInvalidCommand
		}
		var before []byte
		if e := tx.QueryRow(ctx, `SELECT to_jsonb(t) FROM story_threads t WHERE timeline_id=$1 AND id=$2 AND metadata->>'kind'='objective'`, c.TimelineID, p.ObjectiveID).Scan(&before); e != nil {
			return nil, nil, "objective", e
		}
		if _, e := tx.Exec(ctx, `UPDATE story_threads SET status='abandoned',metadata=jsonb_set(metadata,'{evidence}',to_jsonb('Убрано режиссёром'::text),true),last_touched_at_seq=(SELECT head_event_seq+1 FROM timelines WHERE id=$1),updated_at=now() WHERE timeline_id=$1 AND (id=$2 OR metadata->>'parentObjectiveId'=$2::text) AND metadata->>'kind'='objective'`, c.TimelineID, p.ObjectiveID); e != nil {
			return nil, nil, "objective", e
		}
		after, _ := json.Marshal(p)
		return before, after, "objective", nil
	case director.UpsertJournalEntry:
		var p struct {
			EntryID, Category, Name, Description, Level, Status, Evidence string
			Quantity                                                      int
			Tags                                                          []string
		}
		if json.Unmarshal(c.Payload, &p) != nil || p.Name == "" || (p.Category != "ability" && p.Category != "attribute" && p.Category != "item" && p.Category != "currency") || p.Quantity < 0 {
			return nil, nil, "journal_entry", director.ErrInvalidCommand
		}
		if p.Status == "" {
			p.Status = "active"
		}
		if p.Status != "active" && p.Status != "inactive" || p.Category == "item" && p.Status == "active" && p.Quantity < 1 {
			return nil, nil, "journal_entry", director.ErrInvalidCommand
		}
		if p.Evidence == "" {
			p.Evidence = "Исправлено режиссёром"
		}
		threadStatus := "open"
		if p.Status == "inactive" {
			threadStatus = "closed"
		}
		importance := .65
		switch p.Category {
		case "ability":
			importance = .75
		case "attribute", "currency":
			importance = .7
		}
		metadata, _ := json.Marshal(map[string]any{"kind": "hero_journal", "category": p.Category, "quantity": p.Quantity, "level": p.Level, "evidence": p.Evidence, "tags": p.Tags, "source": "director"})
		var before []byte
		var exists bool
		if p.EntryID != "" {
			_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM story_threads WHERE timeline_id=$1 AND id=$2 AND metadata->>'kind'='hero_journal')`, c.TimelineID, p.EntryID).Scan(&exists)
		}
		if p.EntryID == "" || !exists {
			fresh, err := id.New()
			if err != nil {
				return nil, nil, "journal_entry", err
			}
			if p.EntryID == "" {
				p.EntryID = fresh.String()
			}
			var head int64
			if e := tx.QueryRow(ctx, `SELECT head_event_seq FROM timelines WHERE id=$1`, c.TimelineID).Scan(&head); e != nil {
				return nil, nil, "journal_entry", e
			}
			if _, e := tx.Exec(ctx, `INSERT INTO story_threads(id,timeline_id,title,summary,importance,status,metadata,introduced_at_seq,last_touched_at_seq) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8)`, p.EntryID, c.TimelineID, p.Name, p.Description, importance, threadStatus, metadata, head+1); e != nil {
				return nil, nil, "journal_entry", e
			}
		} else {
			if e := tx.QueryRow(ctx, `SELECT to_jsonb(t) FROM story_threads t WHERE timeline_id=$1 AND id=$2 AND metadata->>'kind'='hero_journal'`, c.TimelineID, p.EntryID).Scan(&before); e != nil {
				return nil, nil, "journal_entry", e
			}
			if _, e := tx.Exec(ctx, `UPDATE story_threads SET title=$3,summary=$4,importance=$5,status=$6,metadata=$7,last_touched_at_seq=(SELECT head_event_seq+1 FROM timelines WHERE id=$1),updated_at=now() WHERE timeline_id=$1 AND id=$2`, c.TimelineID, p.EntryID, p.Name, p.Description, importance, threadStatus, metadata); e != nil {
				return nil, nil, "journal_entry", e
			}
		}
		after, _ := json.Marshal(p)
		return before, after, "journal_entry", nil
	case director.UpsertWorldSystem:
		var p struct {
			SystemID    string          `json:"systemId"`
			Name        string          `json:"name"`
			Kind        string          `json:"kind"`
			Description string          `json:"description"`
			Status      string          `json:"status"`
			Resources   json.RawMessage `json:"resources"`
			Version     int64           `json:"version"`
		}
		if json.Unmarshal(c.Payload, &p) != nil || strings.TrimSpace(p.SystemID) == "" || strings.TrimSpace(p.Name) == "" {
			return nil, nil, "world_system", director.ErrInvalidCommand
		}
		if p.Kind == "" {
			p.Kind = "other"
		}
		if p.Kind != "world" && p.Kind != "magic" && p.Kind != "neural" && p.Kind != "technology" && p.Kind != "divine" && p.Kind != "mental" && p.Kind != "social" && p.Kind != "other" {
			return nil, nil, "world_system", director.ErrInvalidCommand
		}
		if p.Status == "" {
			p.Status = "active"
		}
		if p.Status != "active" && p.Status != "pending" && p.Status != "archived" {
			return nil, nil, "world_system", director.ErrInvalidCommand
		}
		if len(p.Resources) == 0 || string(p.Resources) == "null" {
			p.Resources = json.RawMessage(`[]`)
		}
		if !json.Valid(p.Resources) {
			return nil, nil, "world_system", director.ErrInvalidCommand
		}
		var before []byte
		_ = tx.QueryRow(ctx, `SELECT to_jsonb(s) FROM world_systems s WHERE timeline_id=$1 AND system_id=$2`, c.TimelineID, p.SystemID).Scan(&before)
		if len(before) == 0 {
			p.Version = 1
		} else {
			var current struct{ Version int64 }
			_ = json.Unmarshal(before, &current)
			p.Version = current.Version + 1
		}
		_, e := tx.Exec(ctx, `INSERT INTO world_systems(timeline_id,system_id,name,kind,description,resources,status,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(timeline_id,system_id) DO UPDATE SET name=EXCLUDED.name,kind=EXCLUDED.kind,description=EXCLUDED.description,resources=EXCLUDED.resources,status=EXCLUDED.status,version=EXCLUDED.version,updated_at=now()`, c.TimelineID, p.SystemID, p.Name, p.Kind, p.Description, p.Resources, p.Status, p.Version)
		if e != nil {
			return nil, nil, "world_system", e
		}
		after, _ := json.Marshal(p)
		return before, after, "world_system", nil
	case director.UpsertWorldRule:
		var p struct {
			RuleID, SystemID, Title, Category, Severity, Statement string
			Preconditions, Costs, ForbiddenResults, Exceptions     []string
			Tags                                                   []string
			Visibility, Status, ExceptionOf, Evidence              string
			Version                                                int64
		}
		if json.Unmarshal(c.Payload, &p) != nil || strings.TrimSpace(p.RuleID) == "" || strings.TrimSpace(p.SystemID) == "" || strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Statement) == "" {
			return nil, nil, "world_rule", director.ErrInvalidCommand
		}
		if p.Category == "" {
			p.Category = "law"
		}
		if p.Category != "axiom" && p.Category != "law" && p.Category != "mechanism" && p.Category != "limit" && p.Category != "cost" && p.Category != "progression" && p.Category != "exception" && p.Category != "social" && p.Category != "terminology" {
			return nil, nil, "world_rule", director.ErrInvalidCommand
		}
		if p.Severity == "" {
			p.Severity = "soft"
		}
		if p.Severity != "hard" && p.Severity != "soft" && p.Severity != "mystery" && p.Severity != "belief" {
			return nil, nil, "world_rule", director.ErrInvalidCommand
		}
		if p.Visibility == "" {
			p.Visibility = "canon_only"
		}
		if p.Visibility != "canon_only" && p.Visibility != "known_to_hero" && p.Visibility != "public" && p.Visibility != "hidden" {
			return nil, nil, "world_rule", director.ErrInvalidCommand
		}
		if p.Status == "" {
			p.Status = "established"
		}
		if p.Status != "established" && p.Status != "pending" && p.Status != "superseded" && p.Status != "archived" {
			return nil, nil, "world_rule", director.ErrInvalidCommand
		}
		var systemExists bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM world_systems WHERE timeline_id=$1 AND system_id=$2 AND status<>'archived')`, c.TimelineID, p.SystemID).Scan(&systemExists); e != nil || !systemExists {
			return nil, nil, "world_rule", director.ErrInvalidCommand
		}
		if p.Category == "exception" && p.ExceptionOf == "" {
			return nil, nil, "world_rule", director.ErrInvalidCommand
		}
		var before []byte
		_ = tx.QueryRow(ctx, `SELECT to_jsonb(r) FROM world_rules r WHERE timeline_id=$1 AND rule_id=$2`, c.TimelineID, p.RuleID).Scan(&before)
		if len(before) == 0 {
			p.Version = 1
		} else {
			var current struct{ Version int64 }
			_ = json.Unmarshal(before, &current)
			p.Version = current.Version + 1
		}
		var head int64
		if e := tx.QueryRow(ctx, `SELECT GREATEST(head_event_seq,1) FROM timelines WHERE id=$1`, c.TimelineID).Scan(&head); e != nil {
			return nil, nil, "world_rule", e
		}
		_, e := tx.Exec(ctx, `INSERT INTO world_rules(timeline_id,rule_id,system_id,title,category,severity,statement,preconditions,costs,forbidden_results,exceptions,tags,visibility,status,exception_of,source,evidence,version,established_at_seq) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'director',$16,$17,$18) ON CONFLICT(timeline_id,rule_id) DO UPDATE SET system_id=EXCLUDED.system_id,title=EXCLUDED.title,category=EXCLUDED.category,severity=EXCLUDED.severity,statement=EXCLUDED.statement,preconditions=EXCLUDED.preconditions,costs=EXCLUDED.costs,forbidden_results=EXCLUDED.forbidden_results,exceptions=EXCLUDED.exceptions,tags=EXCLUDED.tags,visibility=EXCLUDED.visibility,status=EXCLUDED.status,exception_of=EXCLUDED.exception_of,source='director',evidence=EXCLUDED.evidence,version=EXCLUDED.version,updated_at=now()`, c.TimelineID, p.RuleID, p.SystemID, p.Title, p.Category, p.Severity, p.Statement, p.Preconditions, p.Costs, p.ForbiddenResults, p.Exceptions, p.Tags, p.Visibility, p.Status, p.ExceptionOf, p.Evidence, p.Version, head)
		if e != nil {
			return nil, nil, "world_rule", e
		}
		afterMap := map[string]any{"ruleId": p.RuleID, "systemId": p.SystemID, "title": p.Title, "category": p.Category, "severity": p.Severity, "statement": p.Statement, "preconditions": p.Preconditions, "costs": p.Costs, "forbiddenResults": p.ForbiddenResults, "exceptions": p.Exceptions, "tags": p.Tags, "visibility": p.Visibility, "status": p.Status, "exceptionOf": p.ExceptionOf, "source": "director", "evidence": p.Evidence, "version": p.Version, "establishedAtSeq": head}
		after, _ := json.Marshal(afterMap)
		return before, after, "world_rule", nil
	case director.ArchiveWorldRule:
		var p struct{ RuleID string }
		if json.Unmarshal(c.Payload, &p) != nil || strings.TrimSpace(p.RuleID) == "" {
			return nil, nil, "world_rule", director.ErrInvalidCommand
		}
		var before []byte
		if e := tx.QueryRow(ctx, `SELECT to_jsonb(r) FROM world_rules r WHERE timeline_id=$1 AND rule_id=$2`, c.TimelineID, p.RuleID).Scan(&before); e != nil {
			return nil, nil, "world_rule", e
		}
		if _, e := tx.Exec(ctx, `UPDATE world_rules SET status='archived',version=version+1,source='director',updated_at=now() WHERE timeline_id=$1 AND rule_id=$2`, c.TimelineID, p.RuleID); e != nil {
			return nil, nil, "world_rule", e
		}
		var after []byte
		if e := tx.QueryRow(ctx, `SELECT jsonb_build_object('ruleId',rule_id,'systemId',system_id,'title',title,'category',category,'severity',severity,'statement',statement,'preconditions',preconditions,'costs',costs,'forbiddenResults',forbidden_results,'exceptions',exceptions,'tags',tags,'visibility',visibility,'status',status,'exceptionOf',exception_of,'source',source,'evidence',evidence,'version',version,'establishedAtSeq',established_at_seq) FROM world_rules WHERE timeline_id=$1 AND rule_id=$2`, c.TimelineID, p.RuleID).Scan(&after); e != nil {
			return nil, nil, "world_rule", e
		}
		return before, after, "world_rule", nil
	case director.UpdateWorldResource:
		var p struct {
			ResourceID   string   `json:"resourceId"`
			SystemID     string   `json:"systemId"`
			OwnerType    string   `json:"ownerType"`
			OwnerID      string   `json:"ownerId"`
			OwnerKey     string   `json:"ownerKey"`
			Name         string   `json:"name"`
			Unit         string   `json:"unit"`
			Visibility   string   `json:"visibility"`
			CurrentValue float64  `json:"currentValue"`
			MinValue     *float64 `json:"minValue"`
			MaxValue     *float64 `json:"maxValue"`
			Version      int64    `json:"version"`
		}
		if json.Unmarshal(c.Payload, &p) != nil || strings.TrimSpace(p.ResourceID) == "" || strings.TrimSpace(p.SystemID) == "" || strings.TrimSpace(p.Name) == "" || (p.MinValue != nil && p.CurrentValue < *p.MinValue) || (p.MaxValue != nil && p.CurrentValue > *p.MaxValue) || (p.MinValue != nil && p.MaxValue != nil && *p.MinValue > *p.MaxValue) {
			return nil, nil, "world_resource", director.ErrInvalidCommand
		}
		if p.OwnerType == "" {
			p.OwnerType = "world"
		}
		if p.OwnerType != "world" && p.OwnerType != "hero" {
			return nil, nil, "world_resource", director.ErrInvalidCommand
		}
		if p.Visibility == "" {
			p.Visibility = "canon_only"
		}
		var systemExists bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM world_systems WHERE timeline_id=$1 AND system_id=$2 AND status<>'archived')`, c.TimelineID, p.SystemID).Scan(&systemExists); e != nil || !systemExists {
			return nil, nil, "world_resource", director.ErrInvalidCommand
		}
		var before []byte
		_ = tx.QueryRow(ctx, `SELECT to_jsonb(r) FROM world_resource_states r WHERE timeline_id=$1 AND resource_id=$2 AND owner_type=$3 AND owner_key=$4`, c.TimelineID, p.ResourceID, p.OwnerType, p.OwnerKey).Scan(&before)
		var current struct{ Version int64 }
		_ = json.Unmarshal(before, &current)
		p.Version = max(current.Version+1, 1)
		if _, e := tx.Exec(ctx, `INSERT INTO world_resource_states(timeline_id,resource_id,system_id,owner_type,owner_id,owner_key,name,unit,current_value,min_value,max_value,visibility,version) VALUES($1,$2,$3,$4,NULL,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(timeline_id,resource_id,owner_type,owner_key) DO UPDATE SET system_id=EXCLUDED.system_id,name=EXCLUDED.name,unit=EXCLUDED.unit,current_value=EXCLUDED.current_value,min_value=EXCLUDED.min_value,max_value=EXCLUDED.max_value,visibility=EXCLUDED.visibility,version=EXCLUDED.version,updated_at=now()`, c.TimelineID, p.ResourceID, p.SystemID, p.OwnerType, p.OwnerKey, p.Name, p.Unit, p.CurrentValue, p.MinValue, p.MaxValue, p.Visibility, p.Version); e != nil {
			return nil, nil, "world_resource", e
		}
		after, _ := json.Marshal(p)
		return before, after, "world_resource", nil
	case director.UpdateInstruction:
		var p struct {
			InstructionID, Text, Scope, Priority, Status string
			CreatedAtSeq                                 int64
		}
		if json.Unmarshal(c.Payload, &p) != nil || p.InstructionID == "" || p.Text == "" || (p.Scope != "next_beat" && p.Scope != "scene" && p.Scope != "chapter" && p.Scope != "temporary" && p.Scope != "persistent") || (p.Priority != "low" && p.Priority != "normal" && p.Priority != "high" && p.Priority != "hard") || (p.Status != "active" && p.Status != "cancelled") {
			return nil, nil, "instruction", director.ErrInvalidCommand
		}
		var before []byte
		if e := tx.QueryRow(ctx, `SELECT to_jsonb(i),created_at_seq FROM director_instructions i WHERE timeline_id=$1 AND id=$2`, c.TimelineID, p.InstructionID).Scan(&before, &p.CreatedAtSeq); e != nil {
			return nil, nil, "instruction", e
		}
		if p.Status == "active" {
			if e := tx.QueryRow(ctx, `SELECT head_event_seq FROM timelines WHERE id=$1`, c.TimelineID).Scan(&p.CreatedAtSeq); e != nil {
				return nil, nil, "instruction", e
			}
		}
		if _, e := tx.Exec(ctx, `UPDATE director_instructions SET instruction_text=$3,scope=$4,priority=$5,status=$6,created_at_seq=$7,expires_at=CASE WHEN $6='active' THEN NULL ELSE expires_at END WHERE timeline_id=$1 AND id=$2`, c.TimelineID, p.InstructionID, p.Text, p.Scope, p.Priority, p.Status, p.CreatedAtSeq); e != nil {
			return nil, nil, "instruction", e
		}
		after, _ := json.Marshal(p)
		return before, after, "instruction", nil
	default:
		return nil, nil, "", director.ErrInvalidCommand
	}
}
func nullID(v id.ID) any {
	if v.IsZero() {
		return nil
	}
	return v
}
func (r *DirectorRepository) AddInstruction(ctx context.Context, c director.InstructionCommand) (narrative.DirectorInstruction, error) {
	if e := c.Validate(); e != nil {
		return narrative.DirectorInstruction{}, e
	}
	iid, e := id.New()
	if e != nil {
		return narrative.DirectorInstruction{}, e
	}
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return narrative.DirectorInstruction{}, e
	}
	defer tx.Rollback(ctx)
	var head int64
	if e = tx.QueryRow(ctx, `SELECT head_event_seq FROM timelines WHERE id=$1 FOR UPDATE`, c.TimelineID).Scan(&head); e != nil {
		return narrative.DirectorInstruction{}, e
	}
	v := narrative.DirectorInstruction{ID: iid, TimelineID: id.ID(c.TimelineID), Text: c.Text, Scope: c.Scope, Priority: c.Priority, Status: "active", CreatedAtSeq: head + 1}
	if _, e = tx.Exec(ctx, `INSERT INTO director_instructions(id,timeline_id,instruction_text,scope,priority,status,created_at_seq) VALUES($1,$2,$3,$4,$5,'active',$6)`, v.ID, c.TimelineID, v.Text, v.Scope, v.Priority, v.CreatedAtSeq); e != nil {
		return narrative.DirectorInstruction{}, e
	}
	eventID, e := id.New()
	if e != nil {
		return narrative.DirectorInstruction{}, e
	}
	payload, _ := json.Marshal(map[string]any{"instructionId": v.ID, "timelineId": v.TimelineID, "text": v.Text, "scope": v.Scope, "priority": v.Priority, "status": v.Status, "createdAtSeq": v.CreatedAtSeq})
	if _, e = tx.Exec(ctx, `INSERT INTO story_events(id,timeline_id,seq,event_type,schema_version,payload,created_at) VALUES($1,$2,$3,'director_instruction_added',1,$4,now())`, eventID, c.TimelineID, v.CreatedAtSeq, payload); e != nil {
		return narrative.DirectorInstruction{}, e
	}
	if _, e = tx.Exec(ctx, `UPDATE timelines SET head_event_seq=$2,semantic_revision=semantic_revision+1,updated_at=now() WHERE id=$1`, c.TimelineID, v.CreatedAtSeq); e != nil {
		return narrative.DirectorInstruction{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return narrative.DirectorInstruction{}, e
	}
	return v, nil
}
func (r *DirectorRepository) ActiveInstructions(ctx context.Context, tid timeline.ID) ([]narrative.DirectorInstruction, error) {
	// Repair legacy instructions created before lifecycle consumption existed.
	// Expiry is based on the relevant semantic event, not merely on head movement:
	// a manual Director edit must not consume an instruction intended for a beat.
	if _, e := r.pool.Exec(ctx, `UPDATE director_instructions instruction
SET status='expired'
WHERE instruction.timeline_id=$1 AND instruction.status='active' AND (
 (instruction.scope IN('next_beat','temporary') AND EXISTS(SELECT 1 FROM story_events event WHERE event.timeline_id=instruction.timeline_id AND event.seq>instruction.created_at_seq AND event.event_type='beat_committed')) OR
 (instruction.scope='scene' AND EXISTS(SELECT 1 FROM story_events event WHERE event.timeline_id=instruction.timeline_id AND event.seq>instruction.created_at_seq AND event.event_type='scene_started')) OR
 (instruction.scope='chapter' AND EXISTS(SELECT 1 FROM story_events event WHERE event.timeline_id=instruction.timeline_id AND event.seq>instruction.created_at_seq AND event.event_type='chapter_created'))
)`, tid); e != nil {
		return nil, e
	}
	return r.listInstructions(ctx, tid, true)
}

func (r *DirectorRepository) listInstructions(ctx context.Context, tid timeline.ID, activeOnly bool) ([]narrative.DirectorInstruction, error) {
	filter := ""
	if activeOnly {
		filter = " AND status='active'"
	}
	rows, e := r.pool.Query(ctx, `SELECT id,timeline_id,instruction_text,scope,priority,status,created_at_seq,COALESCE(expires_at,'null'::jsonb) FROM director_instructions WHERE timeline_id=$1`+filter+` ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'expired' THEN 1 WHEN 'completed' THEN 2 ELSE 3 END,CASE priority WHEN 'hard' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END,created_at DESC`, tid)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []narrative.DirectorInstruction{}
	for rows.Next() {
		var v narrative.DirectorInstruction
		if e = rows.Scan(&v.ID, &v.TimelineID, &v.Text, &v.Scope, &v.Priority, &v.Status, &v.CreatedAtSeq, &v.ExpiresAt); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *DirectorRepository) ListAudit(ctx context.Context, tid timeline.ID, limit int) ([]director.AuditEntry, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, e := r.pool.Query(ctx, `SELECT id,timeline_id,command_type,target_type,target_id,before_state,after_state,note FROM director_audit_log WHERE timeline_id=$1 ORDER BY created_at DESC LIMIT $2`, tid, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []director.AuditEntry{}
	for rows.Next() {
		var v director.AuditEntry
		var before, after []byte
		if e = rows.Scan(&v.ID, &v.TimelineID, &v.CommandType, &v.TargetType, &v.TargetID, &before, &after, &v.Note); e != nil {
			return nil, e
		}
		v.Before = before
		v.After = after
		out = append(out, v)
	}
	return out, rows.Err()
}

var _ = fmt.Sprintf
var _ = errors.Is
