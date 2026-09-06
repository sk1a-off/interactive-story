package role_router

import (
	"context"
	"errors"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

var safeHybridFastRoles = map[string]struct{}{
	"action_interpreter": {},
	"turn_planner":       {},
	"pacing":             {},
	"choices":            {},
}

// Router keeps canon-affecting and prose roles on the quality model while
// sending the small preparatory roles to the fast model.
type Router struct {
	fast     aiport.StoryLLM
	quality  aiport.StoryLLM
	identity aiport.ProviderIdentity
}

func NewSafeHybrid(fast, quality aiport.StoryLLM) (*Router, error) {
	if fast == nil || quality == nil {
		return nil, errors.New("safe hybrid requires fast and quality Story LLM clients")
	}
	identity := quality.Identity()
	identity.Profile = "safe-hybrid"
	return &Router{fast: fast, quality: quality, identity: identity}, nil
}

func (r *Router) Identity() aiport.ProviderIdentity { return r.identity }

func (r *Router) Generate(ctx context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	if _, ok := safeHybridFastRoles[request.Role]; ok {
		response, err := r.fast.Generate(ctx, request)
		if err == nil {
			if response.Provider.Model == "" {
				response.Provider = r.fast.Identity()
			}
			return response, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return response, err
		}
		response, err = r.quality.Generate(ctx, request)
		if response.Provider.Model == "" {
			response.Provider = r.quality.Identity()
		}
		response.EscalatedFrom = r.fast.Identity().Model
		return response, err
	}
	response, err := r.quality.Generate(ctx, request)
	if response.Provider.Model == "" {
		response.Provider = r.quality.Identity()
	}
	return response, err
}
