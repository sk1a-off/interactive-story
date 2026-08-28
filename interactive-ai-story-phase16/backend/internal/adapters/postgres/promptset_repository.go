package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	"github.com/local/interactive-ai-story/backend/internal/domain/promptset"
)

type PromptSetRepository struct{ pool *pgxpool.Pool }

func NewPromptSetRepository(p *pgxpool.Pool) *PromptSetRepository {
	return &PromptSetRepository{pool: p}
}

func (r *PromptSetRepository) CreateRevision(ctx context.Context, c promptset.CreateCommand) (promptset.Revision, error) {
	if err := c.Validate(); err != nil {
		return promptset.Revision{}, err
	}
	prompts, err := json.Marshal(c.Prompts)
	if err != nil {
		return promptset.Revision{}, err
	}
	settings, err := json.Marshal(c.RoleSettings)
	if err != nil {
		return promptset.Revision{}, err
	}
	var out promptset.Revision
	var pRaw, sRaw []byte
	err = r.pool.QueryRow(ctx, `INSERT INTO prompt_set_revisions(prompts,role_settings) VALUES($1,$2)
RETURNING id,revision,prompts,role_settings`, prompts, settings).Scan(&out.ID, &out.Revision, &pRaw, &sRaw)
	if err != nil {
		return promptset.Revision{}, err
	}
	if err = decodePromptSet(&out, pRaw, sRaw); err != nil {
		return promptset.Revision{}, err
	}
	return out, nil
}

func (r *PromptSetRepository) Get(ctx context.Context, revisionID id.ID) (promptset.Revision, error) {
	return r.one(ctx, `SELECT id,revision,prompts,role_settings FROM prompt_set_revisions WHERE id=$1`, revisionID)
}

func (r *PromptSetRepository) Active(ctx context.Context) (promptset.Revision, error) {
	return r.one(ctx, `SELECT p.id,p.revision,p.prompts,p.role_settings
FROM prompt_set_revisions p JOIN prompt_active_set a ON a.prompt_set_revision_id=p.id WHERE a.singleton=true`)
}

func (r *PromptSetRepository) List(ctx context.Context) ([]promptset.Revision, error) {
	rows, err := r.pool.Query(ctx, `SELECT id,revision,prompts,role_settings FROM prompt_set_revisions ORDER BY revision DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]promptset.Revision, 0)
	for rows.Next() {
		var rev promptset.Revision
		var pRaw, sRaw []byte
		if err = rows.Scan(&rev.ID, &rev.Revision, &pRaw, &sRaw); err != nil {
			return nil, err
		}
		if err = decodePromptSet(&rev, pRaw, sRaw); err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

func (r *PromptSetRepository) Activate(ctx context.Context, revisionID id.ID) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM prompt_set_revisions WHERE id=$1)`, revisionID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	if _, err = tx.Exec(ctx, `INSERT INTO prompt_active_set(singleton,prompt_set_revision_id,updated_at)
VALUES(true,$1,now()) ON CONFLICT(singleton) DO UPDATE SET prompt_set_revision_id=EXCLUDED.prompt_set_revision_id,updated_at=now()`, revisionID); err != nil {
		return fmt.Errorf("activate prompt set: %w", err)
	}
	return tx.Commit(ctx)
}

func (r *PromptSetRepository) one(ctx context.Context, query string, args ...any) (promptset.Revision, error) {
	var rev promptset.Revision
	var pRaw, sRaw []byte
	err := r.pool.QueryRow(ctx, query, args...).Scan(&rev.ID, &rev.Revision, &pRaw, &sRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return rev, ErrNotFound
	}
	if err != nil {
		return rev, err
	}
	if err = decodePromptSet(&rev, pRaw, sRaw); err != nil {
		return rev, err
	}
	return rev, nil
}

func decodePromptSet(out *promptset.Revision, promptsRaw, settingsRaw []byte) error {
	if err := json.Unmarshal(promptsRaw, &out.Prompts); err != nil {
		return err
	}
	if err := json.Unmarshal(settingsRaw, &out.RoleSettings); err != nil {
		return err
	}
	return nil
}
