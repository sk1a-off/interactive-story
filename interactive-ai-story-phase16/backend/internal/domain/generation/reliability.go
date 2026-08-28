package generation

import (
	"errors"
	"time"
)

var (
	ErrLeaseLost         = errors.New("generation job lease lost")
	ErrAttemptsExhausted = errors.New("generation job attempts exhausted")
)

type Lease struct {
	Owner     string
	ExpiresAt time.Time
}
type Reliability struct {
	AttemptCount      int
	MaxAttempts       int
	AvailableAt       time.Time
	Lease             *Lease
	CancelRequestedAt *time.Time
	LastError         string
}

func (r Reliability) CanClaim(now time.Time) bool {
	return r.AttemptCount < r.MaxAttempts && !now.Before(r.AvailableAt) &&
		(r.Lease == nil || !r.Lease.ExpiresAt.After(now))
}
func (r *Reliability) Claim(owner string, now time.Time, ttl time.Duration) error {
	if owner == "" || ttl <= 0 || !r.CanClaim(now) {
		return ErrInvalidTransition
	}
	r.AttemptCount++
	r.Lease = &Lease{Owner: owner, ExpiresAt: now.Add(ttl)}
	return nil
}
func (r *Reliability) Heartbeat(owner string, now time.Time, ttl time.Duration) error {
	if r.Lease == nil || r.Lease.Owner != owner || !r.Lease.ExpiresAt.After(now) {
		return ErrLeaseLost
	}
	r.Lease.ExpiresAt = now.Add(ttl)
	return nil
}
func (r *Reliability) Retry(now time.Time, backoff time.Duration, errText string) error {
	if r.AttemptCount >= r.MaxAttempts {
		return ErrAttemptsExhausted
	}
	r.Lease = nil
	r.AvailableAt = now.Add(backoff)
	r.LastError = errText
	return nil
}
func (r *Reliability) RequestCancel(now time.Time) { r.CancelRequestedAt = &now }
func (r Reliability) CancelRequested() bool        { return r.CancelRequestedAt != nil }
