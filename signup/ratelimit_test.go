package main

import (
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	now := time.Unix(1000000, 0)
	rl := newRateLimiter(3, 10*time.Minute, 10*time.Minute)
	rl.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !rl.allow("1.2.3.4") {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}
	if rl.allow("1.2.3.4") {
		t.Fatal("4th attempt should be banned")
	}
	if !rl.allow("5.6.7.8") {
		t.Fatal("other IPs unaffected")
	}

	// Still banned right up to the ban expiry.
	now = now.Add(9 * time.Minute)
	if rl.allow("1.2.3.4") {
		t.Fatal("should still be banned at 9 minutes")
	}

	// Ban lifts; the window restarted when the ban was applied.
	now = now.Add(2 * time.Minute)
	if !rl.allow("1.2.3.4") {
		t.Fatal("should be allowed after ban expires")
	}
}

func TestRateLimiterWindowSlides(t *testing.T) {
	now := time.Unix(1000000, 0)
	rl := newRateLimiter(2, 10*time.Minute, 10*time.Minute)
	rl.now = func() time.Time { return now }

	if !rl.allow("ip") || !rl.allow("ip") {
		t.Fatal("first two should pass")
	}
	now = now.Add(11 * time.Minute) // both attempts age out
	if !rl.allow("ip") {
		t.Fatal("attempts outside the window should not count")
	}
}

func TestRateLimiterCleanup(t *testing.T) {
	now := time.Unix(1000000, 0)
	rl := newRateLimiter(2, 10*time.Minute, 10*time.Minute)
	rl.now = func() time.Time { return now }

	rl.allow("stale")
	now = now.Add(time.Hour)
	rl.cleanup()
	if len(rl.visitors) != 0 {
		t.Fatalf("stale visitors not cleaned up: %d left", len(rl.visitors))
	}
}
