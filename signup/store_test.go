package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T, ttl time.Duration) *store {
	t.Helper()
	s, err := newStore(filepath.Join(t.TempDir(), "state.json"), ttl)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStoreAddTake(t *testing.T) {
	s := testStore(t, time.Hour)

	token, err := s.add(pendingSignup{ScreenName: "Running Man", Email: "rm@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 64 {
		t.Fatalf("token should be 32 random bytes hex-encoded, got len %d", len(token))
	}

	// Same ident (case/space-insensitive) can't queue twice.
	if _, err := s.add(pendingSignup{ScreenName: "runningman", Email: "x@example.com"}); !errors.Is(err, errPendingExists) {
		t.Fatalf("duplicate ident should be rejected, got %v", err)
	}

	p, err := s.take(token)
	if err != nil {
		t.Fatal(err)
	}
	if p.ScreenName != "Running Man" || p.Email != "rm@example.com" {
		t.Fatalf("wrong record back: %+v", p)
	}

	// Tokens are single-use.
	if _, err := s.take(token); !errors.Is(err, errTokenInvalid) {
		t.Fatalf("second take should fail, got %v", err)
	}
}

func TestStoreExpiry(t *testing.T) {
	s := testStore(t, time.Hour)
	now := time.Unix(1000000, 0)
	s.now = func() time.Time { return now }

	token, err := s.add(pendingSignup{ScreenName: "abc", Email: "a@b.co"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	if _, err := s.take(token); !errors.Is(err, errTokenInvalid) {
		t.Fatalf("expired token should fail, got %v", err)
	}
}

func TestStorePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s1, err := newStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	token, err := s1.add(pendingSignup{ScreenName: "abc", Email: "a@b.co"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.recordAccount("abc", "a@b.co"); err != nil {
		t.Fatal(err)
	}

	// A restart must lose neither pending signups nor the email ledger.
	s2, err := newStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.take(token); err != nil {
		t.Fatalf("token should survive restart, got %v", err)
	}
	if email, ok := s2.emailFor("abc"); !ok || email != "a@b.co" {
		t.Fatalf("account email should survive restart, got %q, %v", email, ok)
	}
}

func TestStoreResetTokens(t *testing.T) {
	s := testStore(t, time.Hour)
	now := time.Unix(1000000, 0)
	s.now = func() time.Time { return now }

	token, err := s.addReset("Running Man")
	if err != nil {
		t.Fatal(err)
	}

	// peek doesn't consume.
	if _, err := s.peekReset(token); err != nil {
		t.Fatalf("peek should succeed, got %v", err)
	}

	// A newer request for the same ident invalidates the old token.
	token2, err := s.addReset("runningman")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.peekReset(token); !errors.Is(err, errTokenInvalid) {
		t.Fatalf("old token should be invalidated by newer request, got %v", err)
	}

	r, err := s.takeReset(token2)
	if err != nil {
		t.Fatal(err)
	}
	if r.ScreenName != "runningman" {
		t.Fatalf("wrong record back: %+v", r)
	}
	if _, err := s.takeReset(token2); !errors.Is(err, errTokenInvalid) {
		t.Fatalf("second take should fail, got %v", err)
	}

	// Reset tokens expire on their own (shorter) TTL.
	token3, err := s.addReset("abc")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(resetTTL + time.Minute)
	if _, err := s.takeReset(token3); !errors.Is(err, errTokenInvalid) {
		t.Fatalf("expired reset token should fail, got %v", err)
	}
}

func TestStoreDailyCap(t *testing.T) {
	s := testStore(t, time.Hour)
	now := time.Unix(1000000, 0)
	s.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if err := s.countEmail(3); err != nil {
			t.Fatalf("send %d should pass: %v", i+1, err)
		}
	}
	if err := s.countEmail(3); err == nil {
		t.Fatal("4th send should hit the cap")
	}

	// Counter resets on the next UTC day.
	now = now.Add(24 * time.Hour)
	if err := s.countEmail(3); err != nil {
		t.Fatalf("cap should reset next day: %v", err)
	}
}
