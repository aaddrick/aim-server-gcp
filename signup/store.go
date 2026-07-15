package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// pendingSignup is a registration awaiting email verification. Nothing
// secret lives here: the password is generated only after the email
// verifies, so the state file never holds user-chosen credentials.
type pendingSignup struct {
	ScreenName string    `json:"screen_name"`
	Email      string    `json:"email"`
	CreatedAt  time.Time `json:"created_at"`
}

// pendingReset is a password reset awaiting its email link click.
type pendingReset struct {
	ScreenName string    `json:"screen_name"`
	CreatedAt  time.Time `json:"created_at"`
}

// resetTTL bounds password-reset links. Deliberately shorter than the
// signup token TTL: a reset link grants control of an existing account.
const resetTTL = time.Hour

// storeState is the on-disk shape of the store.
type storeState struct {
	Pending map[string]pendingSignup `json:"pending"`          // token -> signup
	Resets  map[string]pendingReset  `json:"resets,omitempty"` // token -> reset
	// Accounts maps ident screen name -> the email address that verified
	// it, so password resets go to an address the owner provably held.
	Accounts map[string]string `json:"accounts,omitempty"`
	// SentDay/SentCount implement the daily send cap that protects the
	// Resend free-tier quota (100/day).
	SentDay   string `json:"sent_day"`
	SentCount int    `json:"sent_count"`
}

type store struct {
	mu    sync.Mutex
	path  string
	ttl   time.Duration
	state storeState
	now   func() time.Time
}

var (
	errPendingExists = errors.New("a signup for this screen name is already awaiting verification")
	errTokenInvalid  = errors.New("invalid or expired verification link")
)

func newStore(path string, ttl time.Duration) (*store, error) {
	s := &store{
		path: path,
		ttl:  ttl,
		now:  time.Now,
		state: storeState{
			Pending:  map[string]pendingSignup{},
			Resets:   map[string]pendingReset{},
			Accounts: map[string]string{},
		},
	}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// first run
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(data, &s.state); err != nil {
			return nil, err
		}
		if s.state.Pending == nil {
			s.state.Pending = map[string]pendingSignup{}
		}
		if s.state.Resets == nil {
			s.state.Resets = map[string]pendingReset{}
		}
		if s.state.Accounts == nil {
			s.state.Accounts = map[string]string{}
		}
	}
	return s, nil
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// add creates a pending signup and returns its verification token.
func (s *store) add(p pendingSignup) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()

	ident := identScreenName(p.ScreenName)
	for _, existing := range s.state.Pending {
		if identScreenName(existing.ScreenName) == ident {
			return "", errPendingExists
		}
	}

	token, err := newToken()
	if err != nil {
		return "", err
	}

	p.CreatedAt = s.now()
	s.state.Pending[token] = p
	return token, s.persistLocked()
}

// take removes and returns the signup for a token, failing if the token is
// unknown or expired.
func (s *store) take(token string) (pendingSignup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()

	p, ok := s.state.Pending[token]
	if !ok {
		return pendingSignup{}, errTokenInvalid
	}
	delete(s.state.Pending, token)
	return p, s.persistLocked()
}

// remove deletes a pending signup by token (e.g. after a failed email send
// so the user can immediately retry).
func (s *store) remove(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.Pending, token)
	_ = s.persistLocked()
}

// recordAccount remembers which email address verified a screen name.
func (s *store) recordAccount(ident, email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Accounts[ident] = email
	return s.persistLocked()
}

// emailFor returns the verified email on file for a screen name.
func (s *store) emailFor(ident string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email, ok := s.state.Accounts[ident]
	return email, ok
}

// addReset creates a password-reset token. A newer request replaces any
// reset already pending for the same screen name.
func (s *store) addReset(screenName string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()

	ident := identScreenName(screenName)
	for token, r := range s.state.Resets {
		if identScreenName(r.ScreenName) == ident {
			delete(s.state.Resets, token)
		}
	}

	token, err := newToken()
	if err != nil {
		return "", err
	}
	s.state.Resets[token] = pendingReset{ScreenName: screenName, CreatedAt: s.now()}
	return token, s.persistLocked()
}

// peekReset reports whether a reset token is still valid without
// consuming it (the confirm form renders against it before the POST).
func (s *store) peekReset(token string) (pendingReset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()

	r, ok := s.state.Resets[token]
	if !ok {
		return pendingReset{}, errTokenInvalid
	}
	return r, nil
}

// takeReset removes and returns the reset for a token, failing if the
// token is unknown or expired.
func (s *store) takeReset(token string) (pendingReset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()

	r, ok := s.state.Resets[token]
	if !ok {
		return pendingReset{}, errTokenInvalid
	}
	delete(s.state.Resets, token)
	return r, s.persistLocked()
}

// removeReset deletes a pending reset by token (e.g. after a failed email
// send so the user can immediately retry).
func (s *store) removeReset(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.Resets, token)
	_ = s.persistLocked()
}

// countEmail increments the daily send counter, failing once cap is reached.
func (s *store) countEmail(cap int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	day := s.now().UTC().Format("2006-01-02")
	if s.state.SentDay != day {
		s.state.SentDay = day
		s.state.SentCount = 0
	}
	if s.state.SentCount >= cap {
		return errors.New("daily signup email limit reached, try again tomorrow")
	}
	s.state.SentCount++
	return s.persistLocked()
}

func (s *store) pruneLocked() {
	cutoff := s.now().Add(-s.ttl)
	for token, p := range s.state.Pending {
		if p.CreatedAt.Before(cutoff) {
			delete(s.state.Pending, token)
		}
	}
	resetCutoff := s.now().Add(-resetTTL)
	for token, r := range s.state.Resets {
		if r.CreatedAt.Before(resetCutoff) {
			delete(s.state.Resets, token)
		}
	}
}

func (s *store) persistLocked() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
