package generation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	aiport "github.com/local/interactive-ai-story/backend/internal/ports/ai"
)

// ErrRoleDeadline marks a role call that exceeded its deadline while the
// turn itself was still live. The job retries from its checkpoint.
var ErrRoleDeadline = errors.New("role call exceeded its deadline")

// RoleDeadlines bounds every role call so a stalled provider cannot hold a
// turn for the full client timeout (12–20 minutes). Classes:
//   - Writer:    prose roles (writer), the longest legitimate calls;
//   - Evaluator: post-writer extraction roles (world/state/quest evaluators,
//     director) and the canon extractor;
//   - Fast:      small preparatory roles (turn_planner, action_interpreter,
//     pacing, choices);
//   - Default:   everything else, including structured_repair.
//
// The defaults sit above the measured 2026-11-09 Antigravity maximum (~80s)
// and the local llama.cpp client timeout (120s), and below the 159–334s
// provider stalls observed on 2026-11-10. Zero disables a class.
type RoleDeadlines struct {
	Writer    time.Duration
	Evaluator time.Duration
	Fast      time.Duration
	Default   time.Duration
}

func DefaultRoleDeadlines() RoleDeadlines {
	return RoleDeadlines{Writer: 240 * time.Second, Evaluator: 180 * time.Second, Fast: 90 * time.Second, Default: 180 * time.Second}
}

// ParseRoleDeadlines accepts "writer=240s,evaluator=180s,fast=90s,default=180s"
// on top of the defaults; "off" disables every deadline.
func ParseRoleDeadlines(spec string) (RoleDeadlines, error) {
	out := DefaultRoleDeadlines()
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return out, nil
	}
	if strings.EqualFold(spec, "off") {
		return RoleDeadlines{}, nil
	}
	for _, part := range strings.Split(spec, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return RoleDeadlines{}, fmt.Errorf("role deadline %q: expected class=duration", part)
		}
		d, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil || d < 0 {
			return RoleDeadlines{}, fmt.Errorf("role deadline %q: invalid duration", part)
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "writer":
			out.Writer = d
		case "evaluator":
			out.Evaluator = d
		case "fast":
			out.Fast = d
		case "default":
			out.Default = d
		default:
			return RoleDeadlines{}, fmt.Errorf("role deadline %q: unknown class", key)
		}
	}
	return out, nil
}

func (d RoleDeadlines) For(role string) time.Duration {
	switch role {
	case "writer":
		return d.Writer
	case "world_evaluator", "state_evaluator", "quest_evaluator", "director", "canon_extractor":
		return d.Evaluator
	case "turn_planner", "action_interpreter", "pacing", "choices":
		return d.Fast
	default:
		return d.Default
	}
}

// WithDeadlines wraps llm so every Generate call runs under the role's
// deadline. A deadline hit is reported as ErrRoleDeadline (not as a plain
// context error) so callers can tell a stalled provider from a cancelled
// turn.
func WithDeadlines(llm aiport.StoryLLM, deadlines RoleDeadlines) aiport.StoryLLM {
	if llm == nil || deadlines == (RoleDeadlines{}) {
		return llm
	}
	return deadlineLLM{base: llm, deadlines: deadlines}
}

type deadlineLLM struct {
	base      aiport.StoryLLM
	deadlines RoleDeadlines
}

func (l deadlineLLM) Identity() aiport.ProviderIdentity { return l.base.Identity() }

func (l deadlineLLM) Generate(ctx context.Context, request aiport.StoryRequest) (aiport.StoryResponse, error) {
	limit := l.deadlines.For(request.Role)
	if limit <= 0 {
		return l.base.Generate(ctx, request)
	}
	callCtx, cancel := context.WithTimeoutCause(ctx, limit, ErrRoleDeadline)
	defer cancel()
	response, err := l.base.Generate(callCtx, request)
	if err != nil && ctx.Err() == nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(context.Cause(callCtx), ErrRoleDeadline)) {
		return response, fmt.Errorf("%w: role %s after %s", ErrRoleDeadline, request.Role, limit)
	}
	return response, err
}
