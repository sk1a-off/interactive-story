package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type GenerationTarget struct{ pool *pgxpool.Pool }

func NewGenerationTarget(p *pgxpool.Pool) *GenerationTarget { return &GenerationTarget{pool: p} }
func (g *GenerationTarget) RecordRuleAudit(ctx context.Context, tid timeline.ID, generationID id.ID, records []generationtarget.RuleAuditRecord) error {
	if len(records) == 0 {
		return nil
	}
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, record := range records {
		if _, err = tx.Exec(ctx, `INSERT INTO world_rule_audit(timeline_id,generation_id,rule_id,severity,evidence,repair_instruction,status) VALUES($1,$2,$3,$4,$5,$6,$7)`, tid, generationID, record.RuleID, record.Severity, record.Evidence, record.RepairInstruction, record.Status); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (g *GenerationTarget) CurrentWriteTarget(ctx context.Context, tid timeline.ID) (generationtarget.Target, error) {
	var v generationtarget.Target
	e := g.pool.QueryRow(ctx, `
	 SELECT c.id,s.id,c.number,s.number,
	        (SELECT COUNT(*) FROM scenes chapter_scenes WHERE chapter_scenes.chapter_id=c.id),
	        (SELECT COUNT(*) FROM beats chapter_beats JOIN scenes chapter_beat_scenes ON chapter_beat_scenes.id=chapter_beats.scene_id WHERE chapter_beat_scenes.chapter_id=c.id AND chapter_beats.is_active AND chapter_beats.status='committed'),
	        COALESCE((SELECT MAX(position) FROM beats WHERE scene_id=s.id),0)+1,
        c.title,c.goal,s.goal,s.mood,
        COALESCE((SELECT content->>'text' FROM beats WHERE scene_id=s.id AND is_active AND status='committed' ORDER BY position DESC LIMIT 1),''),
        COALESCE((SELECT payload FROM story_setup_components WHERE story_id=t.story_id AND component_key='story_bible'),'{}'::jsonb),
        COALESCE((SELECT payload FROM story_setup_components WHERE story_id=t.story_id AND component_key='player'),'{}'::jsonb),
        COALESCE((SELECT payload FROM story_setup_components WHERE story_id=t.story_id AND component_key='world'),'{}'::jsonb),
        COALESCE((SELECT payload FROM story_setup_components WHERE story_id=t.story_id AND component_key='initial_cast'),'{}'::jsonb),
        COALESCE((SELECT payload FROM story_setup_components WHERE story_id=t.story_id AND component_key='visual_bible'),'{}'::jsonb)
 FROM scenes s
 JOIN chapters c ON c.id=s.chapter_id
 JOIN timelines t ON t.id=c.timeline_id
 WHERE c.timeline_id=$1 AND c.status IN('active','completing') AND s.status IN('active','awaiting_player','completing')
	 ORDER BY c.number DESC,s.number DESC LIMIT 1`, tid).Scan(
		&v.ChapterID, &v.SceneID, &v.ChapterNumber, &v.SceneNumber, &v.ChapterSceneCount, &v.ChapterBeatCount,
		&v.NextBeatPosition, &v.ChapterTitle, &v.ChapterGoal, &v.SceneGoal, &v.SceneMood,
		&v.PreviousBeatText, &v.StoryBible, &v.Player, &v.World, &v.InitialCast, &v.VisualBible,
	)
	if e != nil {
		return v, e
	}
	recentRows, recentErr := g.pool.Query(ctx, `SELECT chapter_number,chapter_title,scene_number,scene_goal,position,text
FROM (
 SELECT c.number chapter_number,c.title chapter_title,s.number scene_number,s.goal scene_goal,b.position,b.content->>'text' text
 FROM chapters c JOIN scenes s ON s.chapter_id=c.id JOIN beats b ON b.scene_id=s.id
 WHERE c.timeline_id=$1 AND b.is_active AND b.status='committed'
 ORDER BY c.number DESC,s.number DESC,b.position DESC
 LIMIT 8
) recent ORDER BY chapter_number,scene_number,position`, tid)
	if recentErr != nil {
		return v, recentErr
	}
	for recentRows.Next() {
		var beat generationtarget.RecentBeat
		if recentErr = recentRows.Scan(&beat.ChapterNumber, &beat.ChapterTitle, &beat.SceneNumber, &beat.SceneGoal, &beat.Position, &beat.Text); recentErr != nil {
			recentRows.Close()
			return v, recentErr
		}
		v.RecentBeats = append(v.RecentBeats, beat)
	}
	if recentErr = recentRows.Err(); recentErr != nil {
		recentRows.Close()
		return v, recentErr
	}
	recentRows.Close()
	// Setup is the starting point, but Director edits are the live branch-local
	// world. Overlay active characters and locations before any generation role
	// receives its context so additions, edits and archives affect the next beat.
	var liveCast, liveLocations []byte
	if e = g.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
      'id',c.id,'name',COALESCE(NULLIF(cs.director_profile->>'name',''),c.name),
      'age',COALESCE((cs.director_profile->>'age')::int,c.adult_age),
      'role',COALESCE(cs.director_profile->>'role',c.core->>'role',''),
      'personality',COALESCE(cs.director_profile->>'personality',c.core->>'personality',c.core->>'character',''),
      'relationship',COALESCE(cs.director_profile->>'relationship',c.core->>'relationship',c.core->>'relationshipToHero',''),
      'visualAnchorEn',COALESCE(cs.director_profile->>'visualAnchorEn',c.visual_profile->>'anchorEn',c.core->>'visualAnchorEn',''),
      'mood',cs.mood,'currentGoal',cs.current_goal
    ) ORDER BY COALESCE(NULLIF(cs.director_profile->>'name',''),c.name)),'[]'::jsonb)
    FROM character_states cs JOIN characters c ON c.id=cs.character_id
    WHERE cs.timeline_id=$1 AND cs.active AND c.kind<>'player'`, tid).Scan(&liveCast); e != nil {
		return v, e
	}
	if e = g.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
      'id',l.id,'name',COALESCE(NULLIF(ls.name_override,''),l.name),
      'description',COALESCE(NULLIF(ls.description_override,''),l.description),
      'visualAnchorEn',COALESCE(NULLIF(ls.visual_profile_override->>'anchorEn',''),l.visual_profile->>'anchorEn','')
    ) ORDER BY COALESCE(NULLIF(ls.name_override,''),l.name)),'[]'::jsonb)
    FROM location_states ls JOIN locations l ON l.id=ls.location_id
    WHERE ls.timeline_id=$1 AND ls.active`, tid).Scan(&liveLocations); e != nil {
		return v, e
	}
	v.InitialCast, _ = json.Marshal(map[string]any{"characters": json.RawMessage(liveCast)})
	var world map[string]any
	if json.Unmarshal(v.World, &world) != nil || world == nil {
		world = map[string]any{}
	}
	var locations any
	_ = json.Unmarshal(liveLocations, &locations)
	world["locations"] = locations
	v.World, _ = json.Marshal(world)
	var playerProfile []byte
	if err := g.pool.QueryRow(ctx, `SELECT cs.director_profile FROM character_states cs JOIN characters c ON c.id=cs.character_id WHERE cs.timeline_id=$1 AND c.kind='player' LIMIT 1`, tid).Scan(&playerProfile); err == nil {
		var player, overlay map[string]any
		_ = json.Unmarshal(v.Player, &player)
		_ = json.Unmarshal(playerProfile, &overlay)
		if player == nil {
			player = map[string]any{}
		}
		for key, value := range overlay {
			player[key] = value
		}
		v.Player, _ = json.Marshal(player)
	}
	rows, err := g.pool.Query(ctx, `SELECT id::text,title,summary,status,metadata
FROM story_threads
WHERE timeline_id=$1 AND metadata->>'kind'='objective'
  AND (status IN('open','dormant','resolving') OR id IN (
    SELECT id FROM story_threads
    WHERE timeline_id=$1 AND metadata->>'kind'='objective' AND status IN('closed','abandoned')
    ORDER BY last_touched_at_seq DESC LIMIT 12
  ))
ORDER BY CASE metadata->>'scope' WHEN 'global' THEN 0 ELSE 1 END, importance DESC, introduced_at_seq`, tid)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	hasGlobal, hasMinor := false, false
	for rows.Next() {
		var objective generationtarget.Objective
		var threadStatus string
		var metadata []byte
		if err = rows.Scan(&objective.ID, &objective.Title, &objective.Description, &threadStatus, &metadata); err != nil {
			return v, err
		}
		var meta struct {
			Scope             string `json:"scope"`
			Kind              string `json:"objectiveKind"`
			QuestType         string `json:"questType"`
			ParentObjectiveID string `json:"parentObjectiveId"`
			SuccessCriteria   string `json:"successCriteria"`
			Progress          int    `json:"progress"`
			Evidence          string `json:"evidence"`
		}
		_ = json.Unmarshal(metadata, &meta)
		objective.Scope, objective.Kind, objective.QuestType, objective.ParentObjectiveID, objective.SuccessCriteria, objective.Progress, objective.Evidence = meta.Scope, meta.Kind, meta.QuestType, meta.ParentObjectiveID, meta.SuccessCriteria, meta.Progress, meta.Evidence
		if objective.Kind == "" {
			if objective.Scope == "global" {
				objective.Kind = "quest"
			} else {
				objective.Kind = "task"
			}
		}
		if objective.QuestType == "" {
			objective.QuestType = "main"
		}
		objective.Status = "active"
		if threadStatus == "closed" {
			objective.Status = "completed"
		}
		if threadStatus == "abandoned" {
			objective.Status = "failed"
		}
		hasGlobal = hasGlobal || objective.Scope == "global"
		hasMinor = hasMinor || objective.Scope == "minor"
		v.Objectives = append(v.Objectives, objective)
	}
	if err = rows.Err(); err != nil {
		return v, err
	}
	journalRows, err := g.pool.Query(ctx, `SELECT id::text,title,summary,status,metadata
FROM story_threads
WHERE timeline_id=$1 AND metadata->>'kind'='hero_journal'
ORDER BY CASE metadata->>'category' WHEN 'ability' THEN 0 ELSE 1 END,
         CASE status WHEN 'open' THEN 0 ELSE 1 END,last_touched_at_seq DESC`, tid)
	if err != nil {
		return v, err
	}
	defer journalRows.Close()
	for journalRows.Next() {
		var entry generationtarget.JournalEntry
		var threadStatus string
		var metadata []byte
		if err = journalRows.Scan(&entry.ID, &entry.Name, &entry.Description, &threadStatus, &metadata); err != nil {
			return v, err
		}
		var meta struct {
			Category string   `json:"category"`
			Quantity int      `json:"quantity"`
			Level    string   `json:"level"`
			Evidence string   `json:"evidence"`
			Tags     []string `json:"tags"`
		}
		_ = json.Unmarshal(metadata, &meta)
		entry.Category, entry.Quantity, entry.Level, entry.Evidence, entry.Tags = meta.Category, meta.Quantity, meta.Level, meta.Evidence, meta.Tags
		entry.Status = "active"
		if threadStatus == "closed" || threadStatus == "abandoned" {
			entry.Status = "inactive"
		}
		v.Journal = append(v.Journal, entry)
	}
	if err = journalRows.Err(); err != nil {
		return v, err
	}
	// Timelines created before objective tracking still expose useful goals
	// immediately. The evaluator canonizes these virtual objectives on the next turn.
	if !hasGlobal && v.ChapterGoal != "" {
		v.Objectives = append(v.Objectives, generationtarget.Objective{ID: "virtual-global", Scope: "global", Kind: "quest", QuestType: "main", Title: v.ChapterGoal, SuccessCriteria: v.ChapterGoal, Status: "active", Progress: 0, Virtual: true})
	}
	if !hasMinor && v.SceneGoal != "" && v.SceneGoal != v.ChapterGoal {
		v.Objectives = append(v.Objectives, generationtarget.Objective{ID: "virtual-minor", ParentObjectiveID: "virtual-global", Scope: "minor", Kind: "task", QuestType: "main", Title: v.SceneGoal, SuccessCriteria: v.SceneGoal, Status: "active", Progress: 0, Virtual: true})
	}
	systemRows, err := g.pool.Query(ctx, `SELECT system_id,name,kind,description,status,resources,version FROM world_systems WHERE timeline_id=$1 AND status<>'archived' ORDER BY system_id`, tid)
	if err != nil {
		return v, err
	}
	for systemRows.Next() {
		var system generationtarget.WorldSystem
		var resources []byte
		if err = systemRows.Scan(&system.ID, &system.Name, &system.Kind, &system.Description, &system.Status, &resources, &system.Version); err != nil {
			systemRows.Close()
			return v, err
		}
		system.Resources = json.RawMessage(resources)
		v.WorldSystems = append(v.WorldSystems, system)
	}
	if err = systemRows.Err(); err != nil {
		systemRows.Close()
		return v, err
	}
	systemRows.Close()
	ruleRows, err := g.pool.Query(ctx, `SELECT rule_id,system_id,title,category,severity,statement,preconditions,costs,forbidden_results,exceptions,tags,visibility,status,exception_of,source,evidence,version FROM world_rules WHERE timeline_id=$1 AND status IN('established','pending') ORDER BY CASE severity WHEN 'hard' THEN 0 WHEN 'mystery' THEN 1 ELSE 2 END,rule_id`, tid)
	if err != nil {
		return v, err
	}
	for ruleRows.Next() {
		var rule generationtarget.WorldRule
		var pre, costs, forbidden, exceptions, tags []byte
		if err = ruleRows.Scan(&rule.ID, &rule.SystemID, &rule.Title, &rule.Category, &rule.Severity, &rule.Statement, &pre, &costs, &forbidden, &exceptions, &tags, &rule.Visibility, &rule.Status, &rule.ExceptionOf, &rule.Source, &rule.Evidence, &rule.Version); err != nil {
			ruleRows.Close()
			return v, err
		}
		_ = json.Unmarshal(pre, &rule.Preconditions)
		_ = json.Unmarshal(costs, &rule.Costs)
		_ = json.Unmarshal(forbidden, &rule.ForbiddenResults)
		_ = json.Unmarshal(exceptions, &rule.Exceptions)
		_ = json.Unmarshal(tags, &rule.Tags)
		v.WorldRules = append(v.WorldRules, rule)
	}
	if err = ruleRows.Err(); err != nil {
		ruleRows.Close()
		return v, err
	}
	ruleRows.Close()
	resourceRows, err := g.pool.Query(ctx, `SELECT resource_id,system_id,owner_type,COALESCE(owner_id::text,''),owner_key,name,unit,current_value::float8,min_value::float8,max_value::float8,visibility,version FROM world_resource_states WHERE timeline_id=$1 ORDER BY system_id,resource_id`, tid)
	if err != nil {
		return v, err
	}
	for resourceRows.Next() {
		var resource generationtarget.WorldResource
		if err = resourceRows.Scan(&resource.ID, &resource.SystemID, &resource.OwnerType, &resource.OwnerID, &resource.OwnerKey, &resource.Name, &resource.Unit, &resource.Current, &resource.Minimum, &resource.Maximum, &resource.Visibility, &resource.Version); err != nil {
			resourceRows.Close()
			return v, err
		}
		v.WorldResources = append(v.WorldResources, resource)
	}
	if err = resourceRows.Err(); err != nil {
		resourceRows.Close()
		return v, err
	}
	resourceRows.Close()
	return v, nil
}
