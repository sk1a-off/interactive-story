package imagegeneration

import (
	"testing"
	"time"
)

func TestLeaseAndRetryPolicy(t *testing.T) {
	now := time.Unix(100, 0)
	future := now.Add(time.Minute)
	g := Generation{Status: Pending, Attempts: 0, MaxAttempts: 3, AvailableAt: now}
	if !g.CanClaim(now) {
		t.Fatal("pending job should be claimable")
	}
	g.Status = Running
	g.LeaseUntil = &future
	if g.CanClaim(now) {
		t.Fatal("live lease must not be claimable")
	}
	past := now.Add(-time.Second)
	g.LeaseUntil = &past
	if !g.CanClaim(now) {
		t.Fatal("expired lease should be reclaimable")
	}
	if RetryDelay(1) != 5*time.Second || RetryDelay(2) != 20*time.Second {
		t.Fatal("retry backoff changed")
	}
	if AttemptsExhausted(2, 3) || !AttemptsExhausted(3, 3) {
		t.Fatal("max attempts policy wrong")
	}
}
