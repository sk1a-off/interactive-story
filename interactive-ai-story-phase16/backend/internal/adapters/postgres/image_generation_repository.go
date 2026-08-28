package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/imagegeneration"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/imagegenerationrepo"
)

type ImageGenerationRepository struct{ pool *pgxpool.Pool }

func NewImageGenerationRepository(p *pgxpool.Pool) *ImageGenerationRepository {
	return &ImageGenerationRepository{pool: p}
}

func (r *ImageGenerationRepository) LoadSceneSource(ctx context.Context, storyID, sceneID id.ID) (repo.SceneSource, error) {
	sources, err := r.LoadSceneSources(ctx, storyID, sceneID)
	if err != nil {
		return repo.SceneSource{}, err
	}
	if len(sources) == 0 {
		return repo.SceneSource{}, pgx.ErrNoRows
	}
	return sources[0], nil
}

func (r *ImageGenerationRepository) LoadSceneSources(ctx context.Context, storyID, sceneID id.ID) ([]repo.SceneSource, error) {
	rows, err := r.pool.Query(ctx, `
 SELECT t.story_id,s.id,t.id,b.id,b.position,s.mood,s.goal,COALESCE(b.content->>'text',''),
        COALESCE((SELECT payload::text FROM story_setup_components WHERE story_id=t.story_id AND component_key='story_bible'),'{}'),
        COALESCE((SELECT payload::text FROM story_setup_components WHERE story_id=t.story_id AND component_key='player'),'{}'),
        COALESCE((SELECT payload::text FROM story_setup_components WHERE story_id=t.story_id AND component_key='world'),'{}'),
        COALESCE((SELECT payload::text FROM story_setup_components WHERE story_id=t.story_id AND component_key='initial_cast'),'{}'),
        COALESCE((SELECT payload::text FROM story_setup_components WHERE story_id=t.story_id AND component_key='visual_bible'),'{}'),
        COALESCE((SELECT jsonb_agg(DISTINCT ig.anchor_paragraph ORDER BY ig.anchor_paragraph)
          FROM image_generations ig WHERE ig.scene_id=s.id AND ig.source_beat_id=b.id
            AND ig.anchor_paragraph IS NOT NULL AND ig.status IN('pending','running','done')),'[]'::jsonb)
 FROM scenes s JOIN chapters c ON c.id=s.chapter_id JOIN timelines t ON t.id=c.timeline_id
 JOIN beats b ON b.scene_id=s.id AND b.status='committed' AND b.is_active
 WHERE s.id=$1 AND t.story_id=$2 ORDER BY b.position DESC`, sceneID, storyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []repo.SceneSource{}
	for rows.Next() {
		var v repo.SceneSource
		var excluded []byte
		if err = rows.Scan(&v.StoryID, &v.SceneID, &v.TimelineID, &v.BeatID, &v.BeatPosition, &v.Mood, &v.Goal, &v.Text, &v.StoryBible, &v.Player, &v.World, &v.InitialCast, &v.VisualBible, &excluded); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(excluded, &v.ExcludedParagraphs)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *ImageGenerationRepository) LoadSceneSourceForBeat(ctx context.Context, storyID, sceneID, beatID id.ID) (repo.SceneSource, error) {
	sources, err := r.LoadSceneSources(ctx, storyID, sceneID)
	if err != nil {
		return repo.SceneSource{}, err
	}
	for _, source := range sources {
		if source.BeatID == beatID {
			return source, nil
		}
	}
	return repo.SceneSource{}, pgx.ErrNoRows
}

func (r *ImageGenerationRepository) HasAutomaticMoment(ctx context.Context, sceneID, beatID id.ID, momentIndex int) (bool, error) {
	if beatID.IsZero() || momentIndex < 1 {
		return false, nil
	}
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM image_generations
 WHERE scene_id=$1 AND source_beat_id=$2 AND trigger_kind='automatic' AND moment_index=$3
 )`, sceneID, beatID, momentIndex).Scan(&exists)
	return exists, err
}

func (r *ImageGenerationRepository) ClaimPromptJob(ctx context.Context, worker string, ownerID id.ID, now time.Time, ttl time.Duration) (repo.PromptJob, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return repo.PromptJob{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE image_prompt_jobs SET
 status=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'queued' END,
 lease_owner=NULL,lease_until=NULL,available_at=$1,
 completed_at=CASE WHEN attempts>=max_attempts THEN $1::timestamptz ELSE NULL END,
 last_error=CASE WHEN attempts>=max_attempts AND last_error='' THEN 'lease expired and attempts exhausted' ELSE last_error END
 WHERE status='running' AND lease_until<=$1`, now); err != nil {
		return repo.PromptJob{}, err
	}
	row := tx.QueryRow(ctx, `WITH next AS (
 SELECT j.id FROM image_prompt_jobs j JOIN stories s ON s.id=j.story_id
 WHERE j.status='queued' AND j.available_at<=$1 AND j.attempts<j.max_attempts AND s.owner_id=$4
 ORDER BY j.created_at,j.id FOR UPDATE OF j SKIP LOCKED LIMIT 1
)
UPDATE image_prompt_jobs j SET status='running',attempts=attempts+1,lease_owner=$2,lease_until=$3,
 started_at=COALESCE(started_at,$1),last_error=''
FROM next WHERE j.id=next.id
RETURNING j.id::text,j.story_id::text,j.timeline_id::text,j.scene_id::text,j.source_beat_id::text,
 j.force_illustration,j.attempts,j.max_attempts,j.available_at`, now, worker, now.Add(ttl), ownerID)
	var out repo.PromptJob
	var jobID, storyID, timelineID, sceneID, beatID string
	if err = row.Scan(&jobID, &storyID, &timelineID, &sceneID, &beatID, &out.ForceIllustration, &out.Attempts, &out.MaxAttempts, &out.AvailableAt); errors.Is(err, pgx.ErrNoRows) {
		return repo.PromptJob{}, repo.ErrNoJob
	} else if err != nil {
		return repo.PromptJob{}, err
	}
	if out.ID, err = id.Parse(jobID); err != nil {
		return repo.PromptJob{}, err
	}
	if out.StoryID, err = id.Parse(storyID); err != nil {
		return repo.PromptJob{}, err
	}
	if out.TimelineID, err = id.Parse(timelineID); err != nil {
		return repo.PromptJob{}, err
	}
	if out.SceneID, err = id.Parse(sceneID); err != nil {
		return repo.PromptJob{}, err
	}
	if out.SourceBeatID, err = id.Parse(beatID); err != nil {
		return repo.PromptJob{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return repo.PromptJob{}, err
	}
	return out, nil
}

func (r *ImageGenerationRepository) CompletePromptJob(ctx context.Context, jobID id.ID, worker string, now time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE image_prompt_jobs SET status='completed',lease_owner=NULL,lease_until=NULL,completed_at=$3,last_error=''
 WHERE id=$1 AND status='running' AND lease_owner=$2`, jobID, worker, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return repo.ErrPromptLeaseLost
	}
	return nil
}

func (r *ImageGenerationRepository) FailPromptJob(ctx context.Context, jobID id.ID, worker string, now time.Time, backoff time.Duration, detail string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE image_prompt_jobs SET
 status=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'queued' END,
 lease_owner=NULL,lease_until=NULL,available_at=$3,completed_at=CASE WHEN attempts>=max_attempts THEN $2::timestamptz ELSE NULL END,
 last_error=left($4,2000)
 WHERE id=$1 AND status='running' AND lease_owner=$5`, jobID, now, now.Add(backoff), detail, worker)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return repo.ErrPromptLeaseLost
	}
	return nil
}

func (r *ImageGenerationRepository) Create(ctx context.Context, g domain.Generation) error {
	// pgx handles the concrete ID string reliably; passing *id.ID directly as
	// a nullable UUID parameter is driver-version-sensitive.
	var sourceBeat any
	if g.SourceBeatID != nil {
		sourceBeat = g.SourceBeatID.String()
	}
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if sourceBeat != nil && g.AnchorParagraph > 0 {
		lockKey := fmt.Sprintf("%s|%s|%d", g.SceneID, sourceBeat, g.AnchorParagraph)
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); e != nil {
			return e
		}
		var occupied bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM image_generations WHERE scene_id=$1 AND source_beat_id=$2 AND anchor_paragraph=$3 AND status IN('pending','running','done'))`, g.SceneID, sourceBeat, g.AnchorParagraph).Scan(&occupied); e != nil {
			return e
		}
		if occupied {
			return repo.ErrAnchorOccupied
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO image_generations(
 id,story_id,scene_id,source_beat_id,moment_index,anchor_paragraph,config_revision_id,prompt_set_revision_id,provider_name,model_name,profile_name,
 prompt,negative_prompt,style,style_prompt,style_negative_prompt,width,height,image_count,trigger_kind,status,attempts,max_attempts,available_at,provider_metadata,created_at)
 VALUES($1,$2,$3,$4,$5,NULLIF($6,0),$7,NULLIF($8,'')::uuid,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,'pending',0,$21,$22,
 jsonb_build_object('provider',$9::text,'prompt_set_revision_id',NULLIF($8,''),'moment_index',$5::smallint,'anchor_paragraph',NULLIF($6,0),'style',$14::text,'style_prompt',$15::text,'style_negative_prompt',$16::text,'resolution','768x768','count',$19::smallint),$23)`,
		g.ID, g.StoryID, g.SceneID, sourceBeat, g.MomentIndex, g.AnchorParagraph, g.ConfigRevisionID, g.PromptSetRevisionID, g.Provider.Provider, g.Provider.Model, g.Provider.Profile,
		g.Prompt, g.NegativePrompt, g.Style, g.StylePrompt, g.StyleNegativePrompt, g.Width, g.Height, g.ImageCount, g.Trigger, g.MaxAttempts, g.AvailableAt, g.CreatedAt)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func scanGeneration(row pgx.Row) (domain.Generation, error) {
	var g domain.Generation
	var sourceBeat string
	e := row.Scan(&g.ID, &g.StoryID, &g.SceneID, &sourceBeat, &g.MomentIndex, &g.AnchorParagraph, &g.ConfigRevisionID, &g.PromptSetRevisionID, &g.Provider.Provider, &g.Provider.Model, &g.Provider.Profile,
		&g.Prompt, &g.NegativePrompt, &g.Style, &g.StylePrompt, &g.StyleNegativePrompt, &g.Width, &g.Height, &g.ImageCount, &g.Trigger, &g.Status, &g.Attempts, &g.MaxAttempts,
		&g.AvailableAt, &g.LeaseOwner, &g.LeaseUntil, &g.ErrorCode, &g.CreatedAt, &g.StartedAt, &g.FinishedAt)
	if e != nil {
		return g, e
	}
	if sourceBeat != "" {
		beatID, parseErr := id.Parse(sourceBeat)
		if parseErr != nil {
			return g, parseErr
		}
		g.SourceBeatID = &beatID
	}
	g.Provider.Kind = ai.KindImage
	return g, nil
}

const generationColumns = `id,story_id,scene_id,COALESCE(source_beat_id::text,''),moment_index,COALESCE(anchor_paragraph,0),config_revision_id,COALESCE(prompt_set_revision_id::text,''),provider_name,model_name,profile_name,
prompt,negative_prompt,style,style_prompt,style_negative_prompt,width,height,image_count,trigger_kind,status,attempts,max_attempts,available_at,
COALESCE(lease_owner,''),lease_until,COALESCE(error_code,''),created_at,started_at,finished_at`

func (r *ImageGenerationRepository) Get(ctx context.Context, gid id.ID) (domain.View, error) {
	g, e := scanGeneration(r.pool.QueryRow(ctx, `SELECT `+generationColumns+` FROM image_generations WHERE id=$1`, gid))
	if e != nil {
		return domain.View{}, e
	}
	rows, e := r.pool.Query(ctx, `SELECT id,story_id,scene_id,generation_id,variant,url,content_type,byte_size,created_at FROM scene_images WHERE generation_id=$1 ORDER BY variant`, gid)
	if e != nil {
		return domain.View{}, e
	}
	defer rows.Close()
	out := domain.View{Generation: g, Images: []domain.Image{}}
	for rows.Next() {
		var v domain.Image
		if e = rows.Scan(&v.ID, &v.StoryID, &v.SceneID, &v.GenerationID, &v.Variant, &v.URL, &v.ContentType, &v.ByteSize, &v.CreatedAt); e != nil {
			return domain.View{}, e
		}
		out.Images = append(out.Images, v)
	}
	if e = rows.Err(); e != nil {
		return domain.View{}, e
	}
	_ = r.pool.QueryRow(ctx, `SELECT selected_image_id FROM scenes WHERE id=$1`, g.SceneID).Scan(&out.SelectedImageID)
	return out, nil
}

func (r *ImageGenerationRepository) ClaimNext(ctx context.Context, worker string, now time.Time, ttl time.Duration) (domain.Generation, error) {
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return domain.Generation{}, e
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, `UPDATE image_generations SET
 status=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'pending' END,
 lease_owner=NULL,lease_until=NULL,
 error_code=CASE WHEN attempts>=max_attempts THEN 'attempts_exhausted' ELSE error_code END,
 error_detail=CASE WHEN attempts>=max_attempts THEN COALESCE(error_detail,'lease expired') ELSE error_detail END,
 finished_at=CASE WHEN attempts>=max_attempts THEN $1 ELSE NULL END,
 available_at=$1
 WHERE status='running' AND lease_until<=$1`, now)
	if e != nil {
		return domain.Generation{}, e
	}
	// Claim only the fields required by the browser worker. Avoid scanning the
	// nullable persistence-only columns here: the worker does not need them, and
	// keeping this projection explicit makes the claim path robust across pgx
	// nullable UUID/timestamp handling.
	row := tx.QueryRow(ctx, `
 WITH next AS (
  SELECT id,status FROM image_generations
  WHERE provider_name='perchance_browser' AND (
    (status='running' AND lease_owner=$2 AND lease_until>$1)
    OR
    (status='pending' AND available_at<=$1 AND attempts<max_attempts)
  )
  ORDER BY CASE WHEN status='running' THEN 0 ELSE 1 END,created_at
  FOR UPDATE SKIP LOCKED LIMIT 1
 )
 UPDATE image_generations g SET
 status='running',
 attempts=CASE WHEN next.status='pending' THEN g.attempts+1 ELSE g.attempts END,
 lease_owner=$2,lease_until=$3,
 started_at=COALESCE(started_at,$1),error_code=NULL,error_detail=NULL
 FROM next WHERE g.id=next.id
 RETURNING g.id::text,g.story_id::text,g.scene_id::text,g.config_revision_id::text,
 g.provider_name,g.model_name,g.profile_name,g.prompt,g.negative_prompt,g.style,g.style_prompt,g.style_negative_prompt,
 g.width,g.height,g.image_count::integer,g.trigger_kind,g.status,g.attempts,g.max_attempts,g.available_at,g.created_at`,
		now, worker, now.Add(ttl))

	var g domain.Generation
	var gidText, storyIDText, sceneIDText, configIDText string
	e = row.Scan(
		&gidText, &storyIDText, &sceneIDText, &configIDText,
		&g.Provider.Provider, &g.Provider.Model, &g.Provider.Profile,
		&g.Prompt, &g.NegativePrompt, &g.Style, &g.StylePrompt, &g.StyleNegativePrompt,
		&g.Width, &g.Height, &g.ImageCount, &g.Trigger, &g.Status, &g.Attempts, &g.MaxAttempts,
		&g.AvailableAt, &g.CreatedAt,
	)
	if errors.Is(e, pgx.ErrNoRows) {
		return domain.Generation{}, repo.ErrNoJob
	}
	if e != nil {
		return domain.Generation{}, e
	}
	if g.ID, e = id.Parse(gidText); e != nil {
		return domain.Generation{}, e
	}
	if g.StoryID, e = id.Parse(storyIDText); e != nil {
		return domain.Generation{}, e
	}
	if g.SceneID, e = id.Parse(sceneIDText); e != nil {
		return domain.Generation{}, e
	}
	if g.ConfigRevisionID, e = id.Parse(configIDText); e != nil {
		return domain.Generation{}, e
	}
	g.Provider.Kind = ai.KindImage
	g.LeaseOwner = worker
	leaseUntil := now.Add(ttl)
	g.LeaseUntil = &leaseUntil

	// Pass the UUID as text with an explicit cast. This avoids relying on pgx to
	// encode the project's named string ID type into a nullable uuid column.
	if _, e = tx.Exec(ctx, `INSERT INTO image_worker_state(worker_id,last_seen_at,active_generation_id) VALUES($1,$2,$3::uuid)
 ON CONFLICT(worker_id) DO UPDATE SET last_seen_at=EXCLUDED.last_seen_at,active_generation_id=EXCLUDED.active_generation_id`, worker, now, g.ID.String()); e != nil {
		return domain.Generation{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return domain.Generation{}, e
	}
	return g, nil
}

func (r *ImageGenerationRepository) Complete(ctx context.Context, gid id.ID, worker string, images []domain.Image, now time.Time) (domain.View, error) {
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return domain.View{}, e
	}
	defer tx.Rollback(ctx)
	var status domain.Status
	var owner string
	if e = tx.QueryRow(ctx, `SELECT status,COALESCE(lease_owner,'') FROM image_generations WHERE id=$1 FOR UPDATE`, gid).Scan(&status, &owner); e != nil {
		return domain.View{}, e
	}
	if status == domain.Done {
		_ = tx.Commit(ctx)
		return r.Get(ctx, gid)
	}
	if status != domain.Running || owner != worker {
		return domain.View{}, domain.ErrLeaseMismatch
	}
	if len(images) != 2 {
		return domain.View{}, domain.ErrInvalid
	}
	for _, img := range images {
		if _, e = tx.Exec(ctx, `INSERT INTO scene_images(id,story_id,scene_id,generation_id,variant,url,content_type,byte_size,created_at)
  VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(generation_id,variant) DO NOTHING`,
			img.ID, img.StoryID, img.SceneID, img.GenerationID, img.Variant, img.URL, img.ContentType, img.ByteSize, img.CreatedAt); e != nil {
			return domain.View{}, e
		}
	}
	if _, e = tx.Exec(ctx, `UPDATE image_generations SET status='done',lease_owner=NULL,lease_until=NULL,finished_at=$3,error_code=NULL,error_detail=NULL WHERE id=$1 AND lease_owner=$2`, gid, worker, now); e != nil {
		return domain.View{}, e
	}
	_, _ = tx.Exec(ctx, `UPDATE image_worker_state SET active_generation_id=NULL,last_seen_at=$2 WHERE worker_id=$1`, worker, now)
	if e = tx.Commit(ctx); e != nil {
		return domain.View{}, e
	}
	return r.Get(ctx, gid)
}

func (r *ImageGenerationRepository) Fail(ctx context.Context, gid id.ID, worker string, now time.Time, code, detail string) (domain.Generation, error) {
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return domain.Generation{}, e
	}
	defer tx.Rollback(ctx)
	var attempts, max int
	if e = tx.QueryRow(ctx, `SELECT attempts,max_attempts FROM image_generations WHERE id=$1 AND status='running' AND lease_owner=$2 FOR UPDATE`, gid, worker).Scan(&attempts, &max); e != nil {
		return domain.Generation{}, domain.ErrLeaseMismatch
	}
	terminal := domain.AttemptsExhausted(attempts, max)
	backoff := domain.RetryDelay(attempts)
	if terminal {
		_, e = tx.Exec(ctx, `UPDATE image_generations SET status='failed',lease_owner=NULL,lease_until=NULL,error_code=$3,error_detail=$4,finished_at=$5 WHERE id=$1 AND lease_owner=$2`, gid, worker, code, detail, now)
	} else {
		_, e = tx.Exec(ctx, `UPDATE image_generations SET status='pending',lease_owner=NULL,lease_until=NULL,error_code=$3,error_detail=$4,available_at=$5,finished_at=NULL WHERE id=$1 AND lease_owner=$2`, gid, worker, code, detail, now.Add(backoff))
	}
	if e != nil {
		return domain.Generation{}, e
	}
	_, _ = tx.Exec(ctx, `UPDATE image_worker_state SET active_generation_id=NULL,last_seen_at=$2 WHERE worker_id=$1`, worker, now)
	if e = tx.Commit(ctx); e != nil {
		return domain.Generation{}, e
	}
	return scanGeneration(r.pool.QueryRow(ctx, `SELECT `+generationColumns+` FROM image_generations WHERE id=$1`, gid))
}
func (r *ImageGenerationRepository) Heartbeat(ctx context.Context, worker string, active *id.ID, now time.Time) error {
	activeText := ""
	if active != nil {
		activeText = active.String()
		// Keep a legitimately running browser generation leased while the page is
		// alive. If the tab/userscript reloads, active becomes nil and the next
		// poll can immediately re-deliver the still-owned running job via ClaimNext.
		if _, e := r.pool.Exec(ctx, `UPDATE image_generations
 SET lease_until=$3::timestamptz
 WHERE id=$1::uuid AND status='running' AND lease_owner=$2`,
			activeText, worker, now.Add(5*time.Minute)); e != nil {
			return e
		}
	}
	_, e := r.pool.Exec(ctx, `INSERT INTO image_worker_state(worker_id,last_seen_at,active_generation_id) VALUES($1,$2,NULLIF($3,'')::uuid)
 ON CONFLICT(worker_id) DO UPDATE SET last_seen_at=EXCLUDED.last_seen_at,active_generation_id=EXCLUDED.active_generation_id`, worker, now, activeText)
	return e
}
func (r *ImageGenerationRepository) WorkerStatus(ctx context.Context, now time.Time) (repo.WorkerStatus, error) {
	var v repo.WorkerStatus
	var seen time.Time
	var activeText *string
	e := r.pool.QueryRow(ctx, `SELECT worker_id,last_seen_at,active_generation_id::text FROM image_worker_state ORDER BY last_seen_at DESC LIMIT 1`).Scan(&v.WorkerID, &seen, &activeText)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, nil
	}
	if e != nil {
		return v, e
	}
	if activeText != nil && *activeText != "" {
		activeID, parseErr := id.Parse(*activeText)
		if parseErr != nil {
			return v, parseErr
		}
		v.ActiveGenerationID = &activeID
	}
	v.LastSeenAt = &seen
	v.Connected = now.Sub(seen) < 30*time.Second
	return v, nil
}
func (r *ImageGenerationRepository) SelectImage(ctx context.Context, sceneID, imageID id.ID) error {
	tag, e := r.pool.Exec(ctx, `UPDATE scenes s SET selected_image_id=$2 WHERE s.id=$1 AND EXISTS(SELECT 1 FROM scene_images i WHERE i.id=$2 AND i.scene_id=s.id)`, sceneID, imageID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return errors.New("image does not belong to scene")
	}
	return nil
}
