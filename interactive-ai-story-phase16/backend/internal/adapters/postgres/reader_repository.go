package postgres

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/narrative"
	"github.com/local/interactive-ai-story/backend/internal/domain/timeline"
	"github.com/local/interactive-ai-story/backend/internal/ports/readerrepo"
)

type ReaderRepository struct{ pool *pgxpool.Pool }

func ensureReaderChoices(in []string) []string {
	fallbacks := []string{
		"Осмотреться внимательнее и собрать больше информации",
		"Поговорить с кем-нибудь поблизости",
		"Проверить окружение на необычные детали",
		"Отойти и обдумать ситуацию",
	}
	out := make([]string, 0, 4)
	seen := map[string]struct{}{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || len(out) >= 4 {
			return
		}
		k := strings.ToLower(v)
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		out = append(out, v)
	}
	for _, v := range in {
		add(v)
	}
	for _, v := range fallbacks {
		add(v)
	}
	return out
}

func NewReaderRepository(p *pgxpool.Pool) *ReaderRepository { return &ReaderRepository{pool: p} }
func (r *ReaderRepository) Current(ctx context.Context, tid timeline.ID) (readerrepo.Current, error) {
	var v readerrepo.Current
	var content, choicePayload []byte
	var choiceBeatText string
	e := r.pool.QueryRow(ctx, `
SELECT t.id,t.story_id,t.head_event_seq,c.number,c.title,c.goal,s.id,s.number,s.goal,b.id,b.content,
 COALESCE(choice_event.payload,'{}'::jsonb),COALESCE(choice_event.beat_id,'')
FROM timelines t
JOIN chapters c ON c.timeline_id=t.id AND c.status IN('active','completing')
JOIN scenes s ON s.chapter_id=c.id AND s.status IN('active','awaiting_player','completing')
JOIN beats b ON b.scene_id=s.id AND b.is_active AND b.status='committed'
LEFT JOIN LATERAL (
 SELECT e.payload,
        COALESCE(NULLIF(e.payload->>'beatId',''), previous_beat.payload->>'beatId') AS beat_id
 FROM story_events e
 LEFT JOIN story_events previous_beat
   ON previous_beat.timeline_id=e.timeline_id
  AND previous_beat.seq=e.seq-1
  AND previous_beat.event_type='beat_committed'
 WHERE e.timeline_id=t.id
   AND e.event_type='choices_ready'
   AND COALESCE(NULLIF(e.payload->>'beatId',''), previous_beat.payload->>'beatId')=b.id::text
 ORDER BY e.seq DESC
 LIMIT 1
) choice_event ON true
WHERE t.id=$1 ORDER BY c.number DESC,s.number DESC,b.position DESC LIMIT 1`, tid).
		Scan(&v.TimelineID, &v.StoryID, &v.HeadEventSeq, &v.ChapterNumber, &v.ChapterTitle, &v.ChapterGoal, &v.SceneID, &v.SceneNumber, &v.SceneGoal, &v.BeatID, &content, &choicePayload, &choiceBeatText)
	if e != nil {
		return v, e
	}
	var bc struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(content, &bc)
	v.Text = bc.Text

	// A choice set is valid only when Canon explicitly ties it to the beat the
	// Reader is currently rendering. Older events did not carry beatId, so the
	// lateral query recovers it from the immediately preceding beat_committed
	// event. If no matching event exists we intentionally return no choices;
	// showing a previous turn's choices is worse than briefly showing a pending
	// state while projections catch up.
	if choiceBeatText != "" {
		choiceBeatID, parseErr := id.Parse(choiceBeatText)
		if parseErr != nil {
			return v, parseErr
		}
		var cc struct {
			Choices json.RawMessage `json:"choices"`
		}
		_ = json.Unmarshal(choicePayload, &cc)
		var decoded []string
		if len(cc.Choices) > 0 {
			_ = json.Unmarshal(cc.Choices, &decoded)
			if len(decoded) == 0 {
				var legacy []struct {
					Label string `json:"label"`
				}
				if json.Unmarshal(cc.Choices, &legacy) == nil {
					for _, x := range legacy {
						if x.Label != "" {
							decoded = append(decoded, x.Label)
						}
					}
				}
			}
		}
		decoded = ensureReaderChoices(decoded)
		v.Choices = decoded
		v.ChoiceSet = &readerrepo.ChoiceSet{BeatID: choiceBeatID, Choices: decoded}
	} else {
		v.Choices = []string{}
	}

	v.Objectives = []readerrepo.Objective{}
	objectiveRows, objectiveErr := r.pool.Query(ctx, `SELECT id::text,title,summary,status,metadata
FROM story_threads WHERE timeline_id=$1 AND metadata->>'kind'='objective'
ORDER BY CASE metadata->>'scope' WHEN 'global' THEN 0 ELSE 1 END,
         CASE status WHEN 'open' THEN 0 WHEN 'resolving' THEN 0 ELSE 1 END,
         importance DESC,last_touched_at_seq DESC`, tid)
	if objectiveErr != nil {
		return v, objectiveErr
	}
	hasGlobal, hasMinor := false, false
	for objectiveRows.Next() {
		var objective readerrepo.Objective
		var threadStatus string
		var metadata []byte
		if err := objectiveRows.Scan(&objective.ID, &objective.Title, &objective.Description, &threadStatus, &metadata); err != nil {
			objectiveRows.Close()
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
	if err := objectiveRows.Err(); err != nil {
		objectiveRows.Close()
		return v, err
	}
	objectiveRows.Close()
	v.Abilities, v.Attributes, v.Inventory, v.Currencies = []readerrepo.JournalEntry{}, []readerrepo.JournalEntry{}, []readerrepo.JournalEntry{}, []readerrepo.JournalEntry{}
	journalRows, journalErr := r.pool.Query(ctx, `SELECT id::text,title,summary,status,metadata
FROM story_threads
WHERE timeline_id=$1 AND metadata->>'kind'='hero_journal'
ORDER BY CASE status WHEN 'open' THEN 0 ELSE 1 END,last_touched_at_seq DESC`, tid)
	if journalErr != nil {
		return v, journalErr
	}
	for journalRows.Next() {
		var entry readerrepo.JournalEntry
		var threadStatus string
		var metadata []byte
		if err := journalRows.Scan(&entry.ID, &entry.Name, &entry.Description, &threadStatus, &metadata); err != nil {
			journalRows.Close()
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
		if entry.Category == "ability" {
			v.Abilities = append(v.Abilities, entry)
		} else if entry.Category == "attribute" {
			v.Attributes = append(v.Attributes, entry)
		} else if entry.Category == "item" {
			v.Inventory = append(v.Inventory, entry)
		} else if entry.Category == "currency" {
			v.Currencies = append(v.Currencies, entry)
		}
	}
	if err := journalRows.Err(); err != nil {
		journalRows.Close()
		return v, err
	}
	journalRows.Close()
	v.HeroStats = []readerrepo.HeroStat{}
	statRows, statErr := r.pool.Query(ctx, `SELECT s.key,s.value_type,s.value,c.name
FROM stats s
JOIN characters c ON c.id=s.owner_id AND c.story_id=$2
WHERE s.timeline_id=$1 AND s.owner_type='character' AND c.kind='player'
ORDER BY s.key`, tid, v.StoryID)
	if statErr != nil {
		return v, statErr
	}
	for statRows.Next() {
		var stat readerrepo.HeroStat
		var raw []byte
		if err := statRows.Scan(&stat.Key, &stat.ValueType, &raw, &stat.Name); err != nil {
			statRows.Close()
			return v, err
		}
		_ = json.Unmarshal(raw, &stat.Value)
		v.HeroStats = append(v.HeroStats, stat)
	}
	if err := statRows.Err(); err != nil {
		statRows.Close()
		return v, err
	}
	statRows.Close()
	if !hasGlobal && strings.TrimSpace(v.ChapterGoal) != "" {
		v.Objectives = append(v.Objectives, readerrepo.Objective{ID: "virtual-global", Scope: "global", Kind: "quest", QuestType: "main", Title: v.ChapterGoal, SuccessCriteria: v.ChapterGoal, Status: "active"})
	}
	if !hasMinor && strings.TrimSpace(v.SceneGoal) != "" && strings.TrimSpace(v.SceneGoal) != strings.TrimSpace(v.ChapterGoal) {
		v.Objectives = append(v.Objectives, readerrepo.Objective{ID: "virtual-minor", ParentObjectiveID: "virtual-global", Scope: "minor", Kind: "task", QuestType: "main", Title: v.SceneGoal, SuccessCriteria: v.SceneGoal, Status: "active"})
	}

	beatRows, be := r.pool.Query(ctx, `SELECT id,position,content
 FROM beats
 WHERE scene_id=$1 AND status='committed' AND is_active
 ORDER BY position`, v.SceneID)
	if be != nil {
		return v, be
	}
	v.Beats = []readerrepo.Beat{}
	for beatRows.Next() {
		var beat readerrepo.Beat
		var raw []byte
		if scanErr := beatRows.Scan(&beat.ID, &beat.Position, &raw); scanErr != nil {
			beatRows.Close()
			return v, scanErr
		}
		var body struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(raw, &body)
		beat.Text = body.Text
		beat.Paragraphs = narrative.DisplayParagraphs(body.Text)
		v.Beats = append(v.Beats, beat)
	}
	if be = beatRows.Err(); be != nil {
		beatRows.Close()
		return v, be
	}
	beatRows.Close()

	var selectedImageID *id.ID
	var selectedImageText string
	if r.pool.QueryRow(ctx, `SELECT COALESCE(selected_image_id::text,'') FROM scenes WHERE id=$1`, v.SceneID).Scan(&selectedImageText) == nil && selectedImageText != "" {
		if parsed, parseErr := id.Parse(selectedImageText); parseErr == nil {
			selectedImageID = &parsed
		}
	}
	genRows, ge := r.pool.Query(ctx, `SELECT id,scene_id,COALESCE(source_beat_id::text,''),moment_index,COALESCE(anchor_paragraph,0),status
 FROM image_generations
 WHERE scene_id=$1 AND story_id=$2
 ORDER BY created_at,moment_index`, v.SceneID, v.StoryID)
	if ge != nil {
		return v, ge
	}
	defer genRows.Close()
	v.ImageGenerations = []readerrepo.ImageGeneration{}
	for genRows.Next() {
		var gen readerrepo.ImageGeneration
		var sourceBeat string
		if e = genRows.Scan(&gen.ID, &gen.SceneID, &sourceBeat, &gen.MomentIndex, &gen.AnchorParagraph, &gen.Status); e != nil {
			return v, e
		}
		if sourceBeat != "" {
			parsed, parseErr := id.Parse(sourceBeat)
			if parseErr != nil {
				return v, parseErr
			}
			gen.SourceBeatID = &parsed
		}
		gen.Images = []readerrepo.SceneImage{}
		rows, qe := r.pool.Query(ctx, `SELECT si.id,si.variant,si.url
 FROM scene_images si
 JOIN image_generations ig ON ig.id=si.generation_id
 WHERE si.generation_id=$1 AND si.scene_id=$2 AND ig.scene_id=$2
 ORDER BY si.variant`, gen.ID, v.SceneID)
		if qe != nil {
			return v, qe
		}
		for rows.Next() {
			var im readerrepo.SceneImage
			if scanErr := rows.Scan(&im.ID, &im.Variant, &im.URL); scanErr != nil {
				rows.Close()
				return v, scanErr
			}
			gen.Images = append(gen.Images, im)
			if selectedImageID != nil && im.ID == *selectedImageID {
				x := *selectedImageID
				gen.SelectedImageID = &x
			}
		}
		if qe = rows.Err(); qe != nil {
			rows.Close()
			return v, qe
		}
		rows.Close()
		v.ImageGenerations = append(v.ImageGenerations, gen)
	}
	if e = genRows.Err(); e != nil {
		return v, e
	}
	if len(v.ImageGenerations) > 0 {
		latest := v.ImageGenerations[len(v.ImageGenerations)-1]
		v.ImageGeneration = &latest
	}
	return v, nil
}
