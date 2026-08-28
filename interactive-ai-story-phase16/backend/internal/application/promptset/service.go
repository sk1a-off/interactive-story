package promptset

import (
	"context"
	"strings"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/promptset"
	"github.com/local/interactive-ai-story/backend/internal/ports/promptsetrepo"
	promptcatalog "github.com/local/interactive-ai-story/backend/prompts"
)

type Service struct{ Repo promptsetrepo.Repository }

type StudioView struct {
	Active    domain.SafeView      `json:"active"`
	Revisions []domain.SafeView    `json:"revisions"`
	Builtin   domain.CreateCommand `json:"builtin"`
}

type CreateRequest struct {
	Prompts      map[string]string              `json:"prompts"`
	RoleSettings map[string]domain.RoleSettings `json:"roleSettings"`
	Activate     bool                           `json:"activate"`
}

func BuiltinCommand() (domain.CreateCommand, error) {
	prompts := make(map[string]string, len(domain.Roles))
	settings := make(map[string]domain.RoleSettings, len(domain.Roles))
	for _, role := range domain.Roles {
		text, err := promptcatalog.Get("v1", role)
		if err != nil {
			return domain.CreateCommand{}, err
		}
		prompts[role] = text
		settings[role] = domain.DefaultRoleSettings(role)
	}
	return domain.CreateCommand{Prompts: prompts, RoleSettings: settings}, nil
}

func (s Service) Studio(ctx context.Context) (StudioView, error) {
	active, err := s.Repo.Active(ctx)
	if err != nil {
		return StudioView{}, err
	}
	revisions, err := s.Repo.List(ctx)
	if err != nil {
		return StudioView{}, err
	}
	builtin, err := BuiltinCommand()
	if err != nil {
		return StudioView{}, err
	}
	views := make([]domain.SafeView, 0, len(revisions))
	for _, rev := range revisions {
		views = append(views, safe(rev, rev.ID == active.ID))
	}
	return StudioView{Active: safe(active, true), Revisions: views, Builtin: builtin}, nil
}

func (s Service) Create(ctx context.Context, req CreateRequest) (domain.SafeView, error) {
	cmd := domain.CreateCommand{Prompts: req.Prompts, RoleSettings: req.RoleSettings}
	if err := cmd.Validate(); err != nil {
		return domain.SafeView{}, err
	}
	rev, err := s.Repo.CreateRevision(ctx, cmd)
	if err != nil {
		return domain.SafeView{}, err
	}
	if req.Activate {
		if err = s.Repo.Activate(ctx, rev.ID); err != nil {
			return domain.SafeView{}, err
		}
	}
	return safe(rev, req.Activate), nil
}

func (s Service) Activate(ctx context.Context, revisionID id.ID) (domain.SafeView, error) {
	rev, err := s.Repo.Get(ctx, revisionID)
	if err != nil {
		return domain.SafeView{}, err
	}
	cmd, upgraded, err := completeRevision(rev)
	if err != nil {
		return domain.SafeView{}, err
	}
	if upgraded {
		rev, err = s.Repo.CreateRevision(ctx, cmd)
		if err != nil {
			return domain.SafeView{}, err
		}
		revisionID = rev.ID
	}
	if err = s.Repo.Activate(ctx, revisionID); err != nil {
		return domain.SafeView{}, err
	}
	return safe(rev, true), nil
}

// completeRevision keeps historical prompt revisions usable after the binary
// learns a new role. The old revision remains immutable; activation creates an
// upgraded clone containing builtin defaults only for roles that did not exist
// when that revision was created.
func completeRevision(rev domain.Revision) (domain.CreateCommand, bool, error) {
	builtin, err := BuiltinCommand()
	if err != nil {
		return domain.CreateCommand{}, false, err
	}
	prompts := make(map[string]string, len(domain.Roles))
	settings := make(map[string]domain.RoleSettings, len(domain.Roles))
	for k, v := range rev.Prompts {
		prompts[k] = v
	}
	for k, v := range rev.RoleSettings {
		settings[k] = v
	}
	changed := false
	for _, role := range domain.Roles {
		if strings.TrimSpace(prompts[role]) == "" {
			prompts[role] = builtin.Prompts[role]
			changed = true
		}
		if _, ok := settings[role]; !ok {
			settings[role] = builtin.RoleSettings[role]
			changed = true
		}
	}
	cmd := domain.CreateCommand{Prompts: prompts, RoleSettings: settings}
	if err = cmd.Validate(); err != nil {
		return domain.CreateCommand{}, false, err
	}
	return cmd, changed, nil
}

func safe(rev domain.Revision, active bool) domain.SafeView {
	return domain.SafeView{ID: rev.ID.String(), Revision: rev.Revision, Prompts: rev.Prompts, RoleSettings: rev.RoleSettings, Active: active}
}
