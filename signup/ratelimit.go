package main

import (
	"sync"
	"time"
)

// rateLimiter is a per-IP sliding-window limiter with a temporary ban on
// exceed. The default numbers (10 attempts per 10 minutes, 10-minute ban)
// were tuned on flyspacea prod traffic: legitimate users retry signups
// several times (typos, validation errors), so tighter budgets or hour-long
// bans punish real people more than bots.
type rateLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	banFor   time.Duration
	visitors map[string]*visitor
	now      func() time.Time
}

type visitor struct {
	attempts []time.Time
	banUntil time.Time
}

func newRateLimiter(limit int, window, banFor time.Duration) *rateLimiter {
	return &rateLimiter{
		limit:    limit,
		window:   window,
		banFor:   banFor,
		visitors: map[string]*visitor{},
		now:      time.Now,
	}
}

// allow records an attempt for ip and reports whether it may proceed.
func (r *rateLimiter) allow(ip string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	v, ok := r.visitors[ip]
	if !ok {
		v = &visitor{}
		r.visitors[ip] = v
	}

	if now.Before(v.banUntil) {
		return false
	}

	cutoff := now.Add(-r.window)
	kept := v.attempts[:0]
	for _, t := range v.attempts {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	v.attempts = kept

	if len(v.attempts) >= r.limit {
		v.banUntil = now.Add(r.banFor)
		v.attempts = nil
		return false
	}

	v.attempts = append(v.attempts, now)
	return true
}

// cleanup drops idle visitor entries. Call periodically.
func (r *rateLimiter) cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	for ip, v := range r.visitors {
		if now.After(v.banUntil) && (len(v.attempts) == 0 || now.Sub(v.attempts[len(v.attempts)-1]) > r.window) {
			delete(r.visitors, ip)
		}
	}
}
