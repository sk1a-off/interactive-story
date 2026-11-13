package role_router

import (
	"context"
	"errors"
	"time"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

var safeHybridFastRoles = map[string]struct{}{
	"action_interpreter": {},
	"turn_planner":       {},
	"pacing":             {},
	"choices":            {},
}

// DefaultFastLegTimeout bounds one fast-model attempt. Fast roles complete in
// about one second (2026-11-09 baseline); a call still open after this long
// is a stalled provider, and the quality model answers instead.
const DefaultFastLegTimeout = 45 * time.Second

// Router keeps canon-affecting and prose roles on the quality model while
// sending the small preparatory roles to the fast model.
type Router struct {
	fastState bool
	fast      aiport.StoryLLM
	quality   aiport.StoryLLM
	identity  aiport.ProviderIdentity
	// FastLegTimeout limits the fast attempt before escalation; zero keeps
	// the caller's context only.
	FastLegTimeout time.Duration
}

func NewSafeHybrid(fast, quality aiport.StoryLLM) (*Router, error) {
	return NewSafeHybridWithFastState(fast, quality, false)
}

// NewSafeHybridWithFastState changes only journal evaluation routing.
func NewSafeHybridWithFastState(fast, quality aiport.StoryLLM, enabled bool) (*Router, error) {
	if fast == nil || quality == nil {
		return nil, errors.New("safe hybrid requires fast and quality Story LLM clients")
	}
	identity := quality.Identity()
	identity.Profile = "safe-hybrid"
	return &Router{fast: fast, quality: quality, identity: identity, fastState: enabled, FastLegTimeout: DefaultFastLegTimeout}, nil
}

func (r *Router) Identity() aiport.ProviderIdentity { return r.identity }

func (r *Router) Generate(ctx context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	if _, ok := safeHybridFastRoles[request.Role]; ok || (r.fastState && request.Role == "state_evaluator") {
		response, err := r.generateFast(ctx, request)
		if err == nil {
			if response.Provider.Model == "" {
				response.Provider = r.fast.Identity()
			}
			return response, nil
		}
		// The caller's own cancellation or deadline ends the request. A fast
		// leg that hit FastLegTimeout while the turn is live escalates like any
		// other fast-model error.
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
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

func (r *Router) generateFast(ctx context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	if r.FastLegTimeout <= 0 {
		return r.fast.Generate(ctx, request)
	}
	fastCtx, cancel := context.WithTimeout(ctx, r.FastLegTimeout)
	defer cancel()
	return r.fast.Generate(fastCtx, request)
}
