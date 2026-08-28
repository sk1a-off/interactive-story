package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/local/interactive-ai-story/backend/internal/domain/event"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/setup"
	"github.com/local/interactive-ai-story/backend/internal/domain/story"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/setuprepo"
)

type SetupRepository struct{ pool *pgxpool.Pool }

func NewSetupRepository(pool *pgxpool.Pool) *SetupRepository { return &SetupRepository{pool: pool} }
func (r *SetupRepository) CreateStory(ctx context.Context, s story.Story, tags []string) error {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `INSERT INTO stories(id,owner_id,title,description,status,semantic_revision,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, s.ID, s.OwnerID, s.Title, s.Description, s.Status, s.SemanticRevision, s.CreatedAt, s.UpdatedAt); e != nil {
		return e
	}
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		if _, e = tx.Exec(ctx, `INSERT INTO story_tags(story_id,tag) VALUES($1,$2) ON CONFLICT DO NOTHING`, s.ID, tag); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func (r *SetupRepository) GetStory(ctx context.Context, idv story.ID) (story.Story, error) {
	var s story.Story
	var started *time.Time
	e := r.pool.QueryRow(ctx, `SELECT id,owner_id,title,description,status,semantic_revision,created_at,updated_at,started_at FROM stories WHERE id=$1`, idv).Scan(&s.ID, &s.OwnerID, &s.Title, &s.Description, &s.Status, &s.SemanticRevision, &s.CreatedAt, &s.UpdatedAt, &started)
	if errors.Is(e, pgx.ErrNoRows) {
		return story.Story{}, ErrNotFound
	}
	return s, e
}
func (r *SetupRepository) ListStories(ctx context.Context, ownerID story.UserID) ([]setuprepo.StoryOverview, error) {
	rows, e := r.pool.Query(ctx, `
SELECT s.id,s.owner_id,s.title,s.description,s.status,s.semantic_revision,s.created_at,s.updated_at,
       COALESCE(c.ready_components,0),
       t.id,t.name,t.status,t.head_event_seq,
       GREATEST(s.updated_at,COALESCE(t.updated_at,s.updated_at)) AS last_activity_at
FROM stories s
LEFT JOIN LATERAL (
  SELECT count(*)::integer AS ready_components
  FROM story_setup_components sc
  WHERE sc.story_id=s.id AND sc.status='ready'
) c ON true
LEFT JOIN LATERAL (
  SELECT tl.id,tl.name,tl.status,tl.head_event_seq,tl.updated_at
  FROM timelines tl
  WHERE tl.story_id=s.id AND tl.status<>'deleted'
  ORDER BY (tl.status='active') DESC,tl.updated_at DESC,tl.created_at DESC
  LIMIT 1
) t ON true
WHERE s.owner_id=$1 AND s.status<>'deleted'
ORDER BY last_activity_at DESC,s.created_at DESC`, ownerID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := make([]setuprepo.StoryOverview, 0)
	for rows.Next() {
		var item setuprepo.StoryOverview
		var timelineID, timelineName, timelineStatus *string
		var headEventSeq *int64
		if e = rows.Scan(
			&item.Story.ID, &item.Story.OwnerID, &item.Story.Title, &item.Story.Description,
			&item.Story.Status, &item.Story.SemanticRevision, &item.Story.CreatedAt, &item.Story.UpdatedAt,
			&item.ReadyComponents, &timelineID, &timelineName, &timelineStatus, &headEventSeq, &item.LastActivityAt,
		); e != nil {
			return nil, e
		}
		if timelineID != nil && timelineName != nil && timelineStatus != nil && headEventSeq != nil {
			item.LatestTimeline = &setuprepo.TimelineOverview{
				ID:           timeline.ID(*timelineID),
				Name:         *timelineName,
				Status:       timeline.Status(*timelineStatus),
				HeadEventSeq: *headEventSeq,
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (r *SetupRepository) DeleteStory(ctx context.Context, storyID story.ID, ownerID story.UserID) (bool, error) {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `
UPDATE stories
SET status='deleted',deleted_at=now(),updated_at=now()
WHERE id=$1 AND owner_id=$2 AND status<>'deleted'`, storyID, ownerID)
	if e != nil {
		return false, e
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	if _, e = tx.Exec(ctx, `
UPDATE generation_jobs
SET status='cancelled',completed_at=now(),lease_owner=NULL,lease_expires_at=NULL,
    heartbeat_at=now(),error_code='story_deleted',last_error='story was deleted during setup generation'
WHERE story_id=$1 AND timeline_id IS NULL AND status='running'`, storyID); e != nil {
		return false, e
	}
	return true, tx.Commit(ctx)
}
func (r *SetupRepository) ListComponents(ctx context.Context, sid story.ID) ([]setup.Component, error) {
	rows, e := r.pool.Query(ctx, `SELECT story_id,component_key,revision,source,payload,locked,status,generation_id,updated_at FROM story_setup_components WHERE story_id=$1 ORDER BY component_key`, sid)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []setup.Component
	for rows.Next() {
		var c setup.Component
		var raw []byte
		if e = rows.Scan(&c.StoryID, &c.Key, &c.Revision, &c.Source, &raw, &c.Locked, &c.Status, &c.GenerationID, &c.UpdatedAt); e != nil {
			return nil, e
		}
		c.Payload = json.RawMessage(raw)
		out = append(out, c)
	}
	return out, rows.Err()
}
func (r *SetupRepository) UpsertComponents(ctx context.Context, values []setup.Component) error {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	for _, c := range values {
		if e = c.Validate(); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO story_setup_components(story_id,component_key,revision,source,payload,locked,status,generation_id,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(story_id,component_key) DO UPDATE SET revision=EXCLUDED.revision,source=EXCLUDED.source,payload=EXCLUDED.payload,locked=EXCLUDED.locked,status=EXCLUDED.status,generation_id=EXCLUDED.generation_id,updated_at=EXCLUDED.updated_at`, c.StoryID, c.Key, c.Revision, c.Source, []byte(c.Payload), c.Locked, c.Status, c.GenerationID, c.UpdatedAt)
		if e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func (r *SetupRepository) SetComponent(ctx context.Context, c setup.Component) error {
	if e := c.Validate(); e != nil {
		return e
	}
	tag, e := r.pool.Exec(ctx, `UPDATE story_setup_components SET revision=$3,source=$4,payload=$5,locked=$6,status=$7,generation_id=$8,updated_at=$9 WHERE story_id=$1 AND component_key=$2`, c.StoryID, c.Key, c.Revision, c.Source, []byte(c.Payload), c.Locked, c.Status, c.GenerationID, c.UpdatedAt)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}
func (r *SetupRepository) CreateSetupGeneration(ctx context.Context, gid event.GenerationID, sid story.ID, provider, model, profile string) error {
	tag, e := r.pool.Exec(ctx, `
WITH retired AS (
  UPDATE generation_jobs
  SET status='failed',completed_at=now(),lease_owner=NULL,lease_expires_at=NULL,
      heartbeat_at=now(),error_code='setup_lease_expired',last_error='inline setup generation lease expired'
  WHERE story_id=$2 AND timeline_id IS NULL AND status='running' AND lease_expires_at<now()
)
INSERT INTO generation_jobs(id,story_id,timeline_id,expected_head_event_seq,config_revision_id,prompt_set_revision_id,status,provider_kind,provider_name,model_name,profile_name,attempt_count,max_attempts,available_at,lease_owner,lease_expires_at,heartbeat_at,started_at)
SELECT $1,$2,NULL,0,c.id,p.id,'running','story_llm',$3,$4,$5,1,1,now(),'story-setup-inline',now()+interval '10 minutes',now(),now()
FROM ai_active_config a
JOIN ai_config_revisions c ON c.id=a.config_revision_id
JOIN prompt_active_set pa ON pa.singleton=true
JOIN prompt_set_revisions p ON p.id=pa.prompt_set_revision_id
WHERE a.singleton=true
  AND NOT EXISTS (
    SELECT 1 FROM generation_jobs active
    WHERE active.story_id=$2 AND active.timeline_id IS NULL AND active.status='running'
  )
ON CONFLICT DO NOTHING`, gid, sid, provider, model, profile)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return setuprepo.ErrGenerationActive
	}
	return nil
}
func (r *SetupRepository) FinishSetupGeneration(ctx context.Context, gid event.GenerationID, status string, msg *string) error {
	_, e := r.pool.Exec(ctx, `UPDATE generation_jobs SET status=$2,error_code=$3,completed_at=now(),lease_owner=NULL,lease_expires_at=NULL,heartbeat_at=now() WHERE id=$1`, gid, status, msg)
	return e
}
func (r *SetupRepository) StartStory(ctx context.Context, sid story.ID, tl timeline.Timeline, m setuprepo.StartMaterialization) error {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var started *time.Time
	if e = tx.QueryRow(ctx, `SELECT started_at FROM stories WHERE id=$1 FOR UPDATE`, sid).Scan(&started); e != nil {
		return e
	}
	if started != nil {
		return fmt.Errorf("story already started")
	}
	type initialObjective struct {
		id, parentID                  id.ID
		scope, kind, questType, title string
		description                   string
		successCriteria               string
	}
	objectives := make([]initialObjective, 0)
	for _, quest := range m.Quests {
		oid, err := id.New()
		if err != nil {
			return err
		}
		questType := quest.QuestType
		if questType == "" {
			questType = "main"
		}
		objectives = append(objectives, initialObjective{id: oid, scope: "global", kind: "quest", questType: questType, title: quest.Title, description: quest.Description, successCriteria: quest.SuccessCriteria})
		for _, stage := range quest.Stages {
			stageID, stageErr := id.New()
			if stageErr != nil {
				return stageErr
			}
			objectives = append(objectives, initialObjective{id: stageID, parentID: oid, scope: "minor", kind: stage.Kind, questType: questType, title: stage.Title, description: stage.Description, successCriteria: stage.SuccessCriteria})
		}
	}
	resourceCount := 0
	for _, system := range m.WorldSystems {
		resourceCount += len(system.Resources)
	}
	head := int64(4 + len(objectives) + len(m.WorldSystems) + len(m.WorldRules) + resourceCount)
	if _, e = tx.Exec(ctx, `INSERT INTO timelines(id,story_id,name,status,head_event_seq) VALUES($1,$2,$3,$4,$5)`, tl.ID, tl.StoryID, tl.Name, tl.Status, head); e != nil {
		return e
	}
	for _, character := range m.Characters {
		characterID, err := id.New()
		if err != nil {
			return err
		}
		core, _ := json.Marshal(map[string]any{"role": character.Role, "personality": character.Personality, "relationship": character.Relationship, "visualAnchorEn": character.VisualAnchorEn})
		visual, _ := json.Marshal(map[string]any{"anchorEn": character.VisualAnchorEn})
		if _, e = tx.Exec(ctx, `INSERT INTO characters(id,story_id,kind,name,adult_age,core,visual_profile) VALUES($1,$2,$3,$4,$5,$6,$7)`, characterID, sid, character.Kind, character.Name, character.Age, core, visual); e != nil {
			return e
		}
		profile, _ := json.Marshal(map[string]any{"name": character.Name, "age": character.Age, "role": character.Role, "personality": character.Personality, "relationship": character.Relationship, "visualAnchorEn": character.VisualAnchorEn})
		if _, e = tx.Exec(ctx, `INSERT INTO character_states(timeline_id,character_id,mood,current_goal,active,current_appearance,director_profile,version) VALUES($1,$2,$3,$4,true,'{}',$5,1)`, tl.ID, characterID, character.Mood, character.CurrentGoal, profile); e != nil {
			return e
		}
	}
	for _, location := range m.Locations {
		locationID, err := id.New()
		if err != nil {
			return err
		}
		visual, _ := json.Marshal(map[string]any{"anchorEn": location.VisualAnchorEn})
		if _, e = tx.Exec(ctx, `INSERT INTO locations(id,story_id,name,description,visual_profile) VALUES($1,$2,$3,$4,$5)`, locationID, sid, location.Name, location.Description, visual); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO location_states(timeline_id,location_id,name_override,description_override,visual_profile_override,active,version) VALUES($1,$2,$3,$4,$5,true,1)`, tl.ID, locationID, location.Name, location.Description, visual); e != nil {
			return e
		}
	}
	for _, system := range m.WorldSystems {
		resources, _ := json.Marshal(system.Resources)
		if _, e = tx.Exec(ctx, `INSERT INTO world_systems(timeline_id,system_id,name,kind,description,resources,status,version) VALUES($1,$2,$3,$4,$5,$6,'active',1)`, tl.ID, system.ID, system.Name, system.Kind, system.Description, resources); e != nil {
			return e
		}
		for _, resource := range system.Resources {
			resourceID := system.ID + "." + resource.ID
			ownerType := resource.OwnerScope
			if ownerType != "hero" {
				ownerType = "world"
			}
			if _, e = tx.Exec(ctx, `INSERT INTO world_resource_states(timeline_id,resource_id,system_id,owner_type,owner_id,owner_key,name,unit,current_value,min_value,max_value,visibility,version) VALUES($1,$2,$3,$4,NULL,'',$5,$6,$7,$8,$9,'canon_only',1)`, tl.ID, resourceID, system.ID, ownerType, resource.Name, resource.Unit, resource.InitialValue, resource.MinValue, resource.MaxValue); e != nil {
				return e
			}
		}
	}
	for ruleIndex, rule := range m.WorldRules {
		pre, _ := json.Marshal(rule.Preconditions)
		costs, _ := json.Marshal(rule.Costs)
		forbidden, _ := json.Marshal(rule.ForbiddenResults)
		exceptions, _ := json.Marshal(rule.Exceptions)
		tags, _ := json.Marshal(rule.Tags)
		establishedAtSeq := int64(5 + len(objectives) + len(m.WorldSystems) + ruleIndex)
		if _, e = tx.Exec(ctx, `INSERT INTO world_rules(timeline_id,rule_id,system_id,title,category,severity,statement,preconditions,costs,forbidden_results,exceptions,tags,visibility,status,exception_of,source,evidence,version,established_at_seq) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'setup','',1,$16)`, tl.ID, rule.ID, rule.SystemID, rule.Title, rule.Category, rule.Severity, rule.Statement, pre, costs, forbidden, exceptions, tags, rule.Visibility, rule.Status, rule.ExceptionOf, establishedAtSeq); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO chapters(id,timeline_id,number,title,goal,tone,status) VALUES($1,$2,1,$3,$4,'','active')`, m.ChapterID, tl.ID, m.ChapterTitle, m.ChapterGoal); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO scenes(id,chapter_id,number,goal,status) VALUES($1,$2,1,$3,'awaiting_player')`, m.SceneID, m.ChapterID, m.SceneGoal); e != nil {
		return e
	}
	content, _ := json.Marshal(map[string]any{"text": m.OpeningText})
	if _, e = tx.Exec(ctx, `INSERT INTO beats(id,scene_id,position,kind,content,status,is_active) VALUES($1,$2,1,'mixed',$3,'committed',true)`, m.BeatID, m.SceneID, content); e != nil {
		return e
	}
	e1, e2, e3, e4 := eventIDs4()
	chapterPayload, _ := json.Marshal(map[string]any{"chapterId": m.ChapterID, "number": 1, "title": m.ChapterTitle, "goal": m.ChapterGoal, "tone": "", "status": "active"})
	scenePayload, _ := json.Marshal(map[string]any{"sceneId": m.SceneID, "chapterId": m.ChapterID, "number": 1, "mood": "", "goal": m.SceneGoal, "status": "awaiting_player"})
	opening, _ := json.Marshal(map[string]any{"beatId": m.BeatID, "sceneId": m.SceneID, "position": 1, "kind": "mixed", "text": m.OpeningText, "status": "committed"})
	choices, _ := json.Marshal(map[string]any{"beatId": m.BeatID, "choices": m.Choices})
	if _, e = tx.Exec(ctx, `INSERT INTO story_events(id,timeline_id,seq,event_type,schema_version,payload,created_at) VALUES
	($1,$5,1,'chapter_created',1,$6,now()),
	($2,$5,2,'scene_started',1,$7,now()),
	($3,$5,3,'beat_committed',1,$8,now()),
	($4,$5,4,'choices_ready',1,$9,now())`, e1, e2, e3, e4, tl.ID, chapterPayload, scenePayload, opening, choices); e != nil {
		return e
	}
	for index, objective := range objectives {
		seq := int64(5 + index)
		metadata, _ := json.Marshal(map[string]any{"kind": "objective", "scope": objective.scope, "objectiveKind": objective.kind, "questType": objective.questType, "parentObjectiveId": objective.parentID, "description": objective.description, "successCriteria": objective.successCriteria, "progress": 0, "evidence": "", "source": "setup"})
		importance := .6
		if objective.scope == "global" {
			importance = 1
		}
		if _, e = tx.Exec(ctx, `INSERT INTO story_threads(id,timeline_id,title,summary,importance,status,metadata,introduced_at_seq,last_touched_at_seq) VALUES($1,$2,$3,'',$4,'open',$5,$6,$6)`, objective.id, tl.ID, objective.title, importance, metadata, seq); e != nil {
			return e
		}
		eventID, err := id.New()
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"objectiveId": objective.id, "parentObjectiveId": objective.parentID, "scope": objective.scope, "kind": objective.kind, "questType": objective.questType, "title": objective.title, "description": objective.description, "successCriteria": objective.successCriteria, "status": "active", "progress": 0, "evidence": ""})
		if _, e = tx.Exec(ctx, `INSERT INTO story_events(id,timeline_id,seq,event_type,schema_version,payload,created_at) VALUES($1,$2,$3,'objective_created',1,$4,now())`, eventID, tl.ID, seq, payload); e != nil {
			return e
		}
	}
	seq := int64(5 + len(objectives))
	for _, system := range m.WorldSystems {
		resources, _ := json.Marshal(system.Resources)
		payload, _ := json.Marshal(map[string]any{"systemId": system.ID, "name": system.Name, "kind": system.Kind, "description": system.Description, "resources": json.RawMessage(resources), "status": "active", "version": 1})
		eventID, err := id.New()
		if err != nil {
			return err
		}
		if _, e = tx.Exec(ctx, `INSERT INTO story_events(id,timeline_id,seq,event_type,schema_version,payload,created_at) VALUES($1,$2,$3,'world_system_upserted',1,$4,now())`, eventID, tl.ID, seq, payload); e != nil {
			return e
		}
		seq++
	}
	for _, rule := range m.WorldRules {
		payload, _ := json.Marshal(map[string]any{"ruleId": rule.ID, "systemId": rule.SystemID, "title": rule.Title, "category": rule.Category, "severity": rule.Severity, "statement": rule.Statement, "preconditions": rule.Preconditions, "costs": rule.Costs, "forbiddenResults": rule.ForbiddenResults, "exceptions": rule.Exceptions, "tags": rule.Tags, "visibility": rule.Visibility, "status": rule.Status, "exceptionOf": rule.ExceptionOf, "source": "setup", "evidence": "", "version": 1})
		eventID, err := id.New()
		if err != nil {
			return err
		}
		if _, e = tx.Exec(ctx, `INSERT INTO story_events(id,timeline_id,seq,event_type,schema_version,payload,created_at) VALUES($1,$2,$3,'world_rule_upserted',1,$4,now())`, eventID, tl.ID, seq, payload); e != nil {
			return e
		}
		seq++
	}
	for _, system := range m.WorldSystems {
		for _, resource := range system.Resources {
			ownerType := resource.OwnerScope
			if ownerType != "hero" {
				ownerType = "world"
			}
			payload, _ := json.Marshal(map[string]any{"resourceId": system.ID + "." + resource.ID, "systemId": system.ID, "ownerType": ownerType, "ownerKey": "", "name": resource.Name, "unit": resource.Unit, "currentValue": resource.InitialValue, "minValue": resource.MinValue, "maxValue": resource.MaxValue, "visibility": "canon_only", "version": 1})
			eventID, err := id.New()
			if err != nil {
				return err
			}
			if _, e = tx.Exec(ctx, `INSERT INTO story_events(id,timeline_id,seq,event_type,schema_version,payload,created_at) VALUES($1,$2,$3,'world_resource_changed',1,$4,now())`, eventID, tl.ID, seq, payload); e != nil {
				return e
			}
			seq++
		}
	}
	if _, e = tx.Exec(ctx, `UPDATE stories SET started_at=now(),updated_at=now() WHERE id=$1`, sid); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func eventIDs4() (string, string, string, string) {
	a, _ := newUUID()
	b, _ := newUUID()
	c, _ := newUUID()
	d, _ := newUUID()
	return a, b, c, d
}
func newUUID() (string, error) { x, e := id.New(); return x.String(), e }
