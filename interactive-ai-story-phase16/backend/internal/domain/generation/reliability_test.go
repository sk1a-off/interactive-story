package generation

import (
	"errors"
	"testing"
	"time"
)

func TestLeaseClaimHeartbeatAndExpiry(t *testing.T) {
	now := time.Unix(100, 0)
	r := Reliability{MaxAttempts: 3, AvailableAt: now}
	if e := r.Claim("w1", now, 10*time.Second); e != nil {
		t.Fatal(e)
	}
	if r.AttemptCount != 1 || r.Lease == nil {
		t.Fatal("claim state wrong")
	}
	if e := r.Heartbeat("w1", now.Add(5*time.Second), 10*time.Second); e != nil {
		t.Fatal(e)
	}
	if !r.Lease.ExpiresAt.Equal(now.Add(15 * time.Second)) {
		t.Fatal("heartbeat did not extend lease")
	}
	if e := r.Heartbeat("w2", now.Add(6*time.Second), time.Second); !errors.Is(e, ErrLeaseLost) {
		t.Fatal("foreign worker extended lease")
	}
}
func TestRetryStopsAtMaxAttempts(t *testing.T) {
	now := time.Unix(100, 0)
	r := Reliability{MaxAttempts: 1, AvailableAt: now}
	if e := r.Claim("w", now, time.Second); e != nil {
		t.Fatal(e)
	}
	if e := r.Retry(now, time.Second, "boom"); !errors.Is(e, ErrAttemptsExhausted) {
		t.Fatal("max attempts not enforced")
	}
}
func TestCancellationFlag(t *testing.T) {
	var r Reliability
	n := time.Unix(1, 0)
	r.RequestCancel(n)
	if !r.CancelRequested() {
		t.Fatal("cancel flag missing")
	}
}
