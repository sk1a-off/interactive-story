package openai_compatible

import (
	"context"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"
)

// RequestControl bounds physical HTTP calls, including open stream bodies.
// Pools share cooldown and capacity across keys, models and prompt revisions on
// the same provider/origin. Separate processes still need an external quota gate.
type RequestControl struct {
	mu            sync.Mutex
	hosted, local int
	pools         map[string]*requestPool
	// Tests replace only the delay calculation; admission/cancellation stay real.
	backoff func(int, string) (time.Duration, bool)
}

type requestPool struct {
	active     int
	foreground []*requestTicket
	background []*requestTicket
	notBefore  time.Time
	changed    chan struct{}
}

type requestTicket struct{ background bool }

func NewRequestControl(hosted, local int) *RequestControl {
	return &RequestControl{hosted: max(1, hosted), local: max(1, local), pools: make(map[string]*requestPool)}
}

func requestScope(provider, endpoint string) string {
	u, _ := url.Parse(endpoint) // validated by New
	return provider + "\x00" + strings.ToLower(u.Scheme+"://"+u.Host)
}

func (c *RequestControl) pool(scope string) *requestPool {
	p := c.pools[scope]
	if p == nil {
		p = &requestPool{changed: make(chan struct{})}
		c.pools[scope] = p
	}
	return p
}

func (p *requestPool) notify() {
	close(p.changed)
	p.changed = make(chan struct{})
}

func (p *requestPool) remove(ticket *requestTicket) {
	queue := &p.foreground
	if ticket.background {
		queue = &p.background
	}
	for i, v := range *queue {
		if v == ticket {
			*queue = append((*queue)[:i], (*queue)[i+1:]...)
			break
		}
	}
}

func (c *RequestControl) acquire(ctx context.Context, scope string, hosted, background bool) (func(), error) {
	limit := c.local
	if hosted {
		limit = c.hosted
	}
	ticket := &requestTicket{background: background}
	c.mu.Lock()
	p := c.pool(scope)
	if background {
		p.background = append(p.background, ticket)
	} else {
		p.foreground = append(p.foreground, ticket)
	}
	for {
		if err := ctx.Err(); err != nil {
			p.remove(ticket)
			p.notify()
			c.mu.Unlock()
			return nil, err
		}
		first := len(p.foreground) > 0 && p.foreground[0] == ticket
		if len(p.foreground) == 0 {
			first = len(p.background) > 0 && p.background[0] == ticket
		}
		wait := time.Until(p.notBefore)
		if first && p.active < limit && wait <= 0 {
			p.remove(ticket)
			p.active++
			p.notify()
			c.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					c.mu.Lock()
					p.active--
					p.notify()
					c.mu.Unlock()
				})
			}, nil
		}
		changed := p.changed
		c.mu.Unlock()
		var timer *time.Timer
		var wake <-chan time.Time
		if wait > 0 {
			timer = time.NewTimer(wait)
			wake = timer.C
		}
		select {
		case <-ctx.Done():
		case <-changed:
		case <-wake:
		}
		if timer != nil {
			timer.Stop()
		}
		c.mu.Lock()
	}
}

func (c *RequestControl) cooldown(scope string, delay time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.pool(scope)
	until := time.Now().Add(delay)
	if until.After(p.notBefore) {
		p.notBefore = until
		p.notify()
	}
}

type requestState struct {
	background   bool
	quotaRetries int
}
type requestStateKey struct{}

func requestContext(ctx context.Context, role string) context.Context {
	if ctx.Value(requestStateKey{}) != nil {
		return ctx // protocol fallback retains priority and its bounded retry budget
	}
	background := false
	switch role {
	case "image_prompt", "image_moments", "image_next_moment", "image_paragraph":
		background = true
	}
	return context.WithValue(ctx, requestStateKey{}, &requestState{background: background})
}

type permitBody struct {
	io.ReadCloser
	release func()
}

func (b *permitBody) Close() error {
	defer b.release()
	return b.ReadCloser.Close()
}
