package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMgmtAPI imitates the open-oscar-server management API endpoints the
// signup service touches.
type fakeMgmtAPI struct {
	mu    sync.Mutex
	users map[string]string // ident -> password
}

func (f *fakeMgmtAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /user/{screenname}/account", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, ok := f.users[identScreenName(r.PathValue("screenname"))]; !ok {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	})
	mux.HandleFunc("POST /user", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ScreenName string `json:"screen_name"`
			Password   string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "malformed input", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		ident := identScreenName(in.ScreenName)
		if _, ok := f.users[ident]; ok {
			http.Error(w, "user already exists", http.StatusConflict)
			return
		}
		f.users[ident] = in.Password
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("PUT /user/password", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ScreenName string `json:"screen_name"`
			Password   string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "malformed input", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		ident := identScreenName(in.ScreenName)
		if _, ok := f.users[ident]; !ok {
			http.Error(w, "user does not exist", http.StatusNotFound)
			return
		}
		f.users[ident] = in.Password
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// fakeResend records sent emails.
type fakeResend struct {
	mu     sync.Mutex
	emails []resendEmail
	fail   bool
}

func (f *fakeResend) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.fail {
			http.Error(w, `{"message":"nope"}`, http.StatusInternalServerError)
			return
		}
		var e resendEmail
		_ = json.NewDecoder(r.Body).Decode(&e)
		f.mu.Lock()
		f.emails = append(f.emails, e)
		f.mu.Unlock()
		_, _ = io.WriteString(w, `{"id":"fake"}`)
	})
}

func (f *fakeResend) last(t *testing.T) resendEmail {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.emails) == 0 {
		t.Fatal("no emails sent")
	}
	return f.emails[len(f.emails)-1]
}

func testApp(t *testing.T) (*app, *fakeMgmtAPI, *fakeResend) {
	t.Helper()

	mgmt := &fakeMgmtAPI{users: map[string]string{}}
	mgmtSrv := httptest.NewServer(mgmt.handler())
	t.Cleanup(mgmtSrv.Close)

	resend := &fakeResend{}
	resendSrv := httptest.NewServer(resend.handler())
	t.Cleanup(resendSrv.Close)

	st, err := newStore(filepath.Join(t.TempDir(), "state.json"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	mailer := newResendClient("test-key", "AIM <verify@aim.example.com>")
	mailer.baseURL = resendSrv.URL

	return &app{
		baseURL:    "https://aim.example.com",
		serverHost: "aim.example.com",
		mgmtAPI:    mgmtSrv.URL,
		tokenTTL:   time.Hour,
		emailCap:   90,
		store:      st,
		limiter:    newRateLimiter(10, 10*time.Minute, 10*time.Minute),
		mailer:     mailer,
		client:     &http.Client{Timeout: 5 * time.Second},
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, mgmt, resend
}

func postForm(t *testing.T, a *app, path string, form url.Values, ip string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = ip + ":12345"
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, req)
	return w
}

func postSignup(t *testing.T, a *app, form url.Values, ip string) *httptest.ResponseRecorder {
	t.Helper()
	return postForm(t, a, "/signup", form, ip)
}

func get(t *testing.T, a *app, path, ip string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = ip + ":12345"
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, req)
	return w
}

var (
	verifyTokenRe = regexp.MustCompile(`/verify\?token=([0-9a-f]{64})`)
	resetTokenRe  = regexp.MustCompile(`/reset/confirm\?token=([0-9a-f]{64})`)
	shownPassRe   = regexp.MustCompile(`<code>([` + passwordAlphabet + `]{12})</code>`)
)

// signUpAndVerify walks a screen name through the full flow and returns
// the generated password shown on the success page.
func signUpAndVerify(t *testing.T, a *app, resend *fakeResend, screenName, email, ip string) string {
	t.Helper()

	w := postSignup(t, a, url.Values{"screen_name": {screenName}, "email": {email}}, ip)
	if w.Code != http.StatusOK {
		t.Fatalf("signup: got %d: %s", w.Code, w.Body.String())
	}
	m := verifyTokenRe.FindStringSubmatch(resend.last(t).HTML)
	if m == nil {
		t.Fatalf("no verify link in email: %s", resend.last(t).HTML)
	}
	vw := get(t, a, "/verify?token="+m[1], ip)
	if vw.Code != http.StatusOK {
		t.Fatalf("verify: got %d: %s", vw.Code, vw.Body.String())
	}
	pm := shownPassRe.FindStringSubmatch(vw.Body.String())
	if pm == nil {
		t.Fatalf("no generated password on success page: %s", vw.Body.String())
	}
	return pm[1]
}

func TestSignupVerifyFlow(t *testing.T) {
	a, mgmt, resend := testApp(t)

	w := postSignup(t, a, url.Values{
		"screen_name": {"Running Man"},
		"email":       {"rm@example.com"},
	}, "1.2.3.4")
	if w.Code != http.StatusOK {
		t.Fatalf("signup: got %d: %s", w.Code, w.Body.String())
	}

	email := resend.last(t)
	if email.To[0] != "rm@example.com" {
		t.Errorf("email went to %q", email.To[0])
	}
	m := verifyTokenRe.FindStringSubmatch(email.HTML)
	if m == nil {
		t.Fatalf("no verify link in email: %s", email.HTML)
	}

	// Account must not exist before verification.
	mgmt.mu.Lock()
	if len(mgmt.users) != 0 {
		t.Fatal("account created before email verification")
	}
	mgmt.mu.Unlock()

	vw := get(t, a, "/verify?token="+m[1], "1.2.3.4")
	if vw.Code != http.StatusOK {
		t.Fatalf("verify: got %d: %s", vw.Code, vw.Body.String())
	}

	// The success page shows the generated password, and it matches what
	// the account was created with.
	pm := shownPassRe.FindStringSubmatch(vw.Body.String())
	if pm == nil {
		t.Fatalf("no generated password on success page: %s", vw.Body.String())
	}
	mgmt.mu.Lock()
	if pass, ok := mgmt.users["runningman"]; !ok || pass != pm[1] {
		t.Fatalf("account password %q does not match shown password %q", pass, pm[1])
	}
	mgmt.mu.Unlock()

	// The generated password must never hit the state file.
	if strings.Contains(readFile(t, a.store.path), pm[1]) {
		t.Fatal("generated password persisted to state file")
	}

	// Reusing the link must fail.
	if rw := get(t, a, "/verify?token="+m[1], "1.2.3.4"); rw.Code != http.StatusNotFound {
		t.Fatalf("token reuse: got %d, want 404", rw.Code)
	}
}

func TestSignupRejectsTakenName(t *testing.T) {
	a, mgmt, resend := testApp(t)
	mgmt.users["runningman"] = "whatever"

	w := postSignup(t, a, url.Values{
		"screen_name": {"Running Man"},
		"email":       {"rm@example.com"},
	}, "1.2.3.4")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d, want 422", w.Code)
	}
	if len(resend.emails) != 0 {
		t.Fatal("no email should be sent for a taken name")
	}
}

func TestSignupHoneypot(t *testing.T) {
	a, _, resend := testApp(t)

	w := postSignup(t, a, url.Values{
		"screen_name": {"BotName"},
		"email":       {"bot@example.com"},
		"website":     {"https://spam.example"},
	}, "1.2.3.4")
	// Bots get a fake success page and nothing happens.
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	if len(resend.emails) != 0 {
		t.Fatal("honeypot must not trigger email")
	}
}

func TestSignupRateLimit(t *testing.T) {
	a, _, _ := testApp(t)
	form := url.Values{
		"screen_name": {"ab"}, // invalid, but attempts still count
		"email":       {"x@example.com"},
	}
	var last int
	for i := 0; i < 11; i++ {
		last = postSignup(t, a, form, "9.9.9.9").Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("11th attempt: got %d, want 429", last)
	}
	// Different IP unaffected.
	if code := postSignup(t, a, form, "8.8.8.8").Code; code == http.StatusTooManyRequests {
		t.Fatal("other IPs should not be limited")
	}
}

func TestSignupEmailFailureAllowsRetry(t *testing.T) {
	a, _, resend := testApp(t)
	resend.fail = true

	form := url.Values{
		"screen_name": {"Running Man"},
		"email":       {"rm@example.com"},
	}
	if w := postSignup(t, a, form, "1.2.3.4"); w.Code != http.StatusBadGateway {
		t.Fatalf("got %d, want 502", w.Code)
	}

	// The pending slot must have been released for an immediate retry.
	resend.fail = false
	if w := postSignup(t, a, form, "1.2.3.4"); w.Code != http.StatusOK {
		t.Fatalf("retry after email failure: got %d: %s", w.Code, w.Body.String())
	}
}

func TestResetFlow(t *testing.T) {
	a, mgmt, resend := testApp(t)
	oldPass := signUpAndVerify(t, a, resend, "Running Man", "rm@example.com", "1.2.3.4")

	// Request a reset; the link must go to the email that verified the
	// account, regardless of the case/spacing the requester types.
	w := postForm(t, a, "/reset", url.Values{"screen_name": {"runningman"}}, "1.2.3.4")
	if w.Code != http.StatusOK {
		t.Fatalf("reset request: got %d: %s", w.Code, w.Body.String())
	}
	email := resend.last(t)
	if email.To[0] != "rm@example.com" {
		t.Errorf("reset email went to %q", email.To[0])
	}
	m := resetTokenRe.FindStringSubmatch(email.HTML)
	if m == nil {
		t.Fatalf("no reset link in email: %s", email.HTML)
	}

	// The confirm form renders without consuming the token.
	if cw := get(t, a, "/reset/confirm?token="+m[1], "1.2.3.4"); cw.Code != http.StatusOK {
		t.Fatalf("confirm form: got %d", cw.Code)
	}

	// An invalid new password re-renders the form without burning the link.
	if bw := postForm(t, a, "/reset/confirm", url.Values{"token": {m[1]}, "password": {"abc"}}, "1.2.3.4"); bw.Code != http.StatusUnprocessableEntity {
		t.Fatalf("short password: got %d, want 422", bw.Code)
	}

	cw := postForm(t, a, "/reset/confirm", url.Values{"token": {m[1]}, "password": {"hunter2new"}}, "1.2.3.4")
	if cw.Code != http.StatusOK {
		t.Fatalf("confirm: got %d: %s", cw.Code, cw.Body.String())
	}

	mgmt.mu.Lock()
	if pass := mgmt.users["runningman"]; pass != "hunter2new" {
		t.Fatalf("password not changed: %q (old was %q)", pass, oldPass)
	}
	mgmt.mu.Unlock()

	// The new password must never hit the state file.
	if strings.Contains(readFile(t, a.store.path), "hunter2new") {
		t.Fatal("new password persisted to state file")
	}

	// Reset tokens are single-use.
	if rw := postForm(t, a, "/reset/confirm", url.Values{"token": {m[1]}, "password": {"another1"}}, "1.2.3.4"); rw.Code != http.StatusNotFound {
		t.Fatalf("token reuse: got %d, want 404", rw.Code)
	}
}

func TestResetUnknownAccountStaysQuiet(t *testing.T) {
	a, _, resend := testApp(t)

	// No account, no email on file: same neutral page, no email sent.
	w := postForm(t, a, "/reset", url.Values{"screen_name": {"Nobody Here"}}, "1.2.3.4")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	if len(resend.emails) != 0 {
		t.Fatal("no email should be sent for an unknown account")
	}
}

func TestClientIP(t *testing.T) {
	// Direct (non-loopback) connections ignore XFF entirely.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:555"
	r.Header.Set("X-Forwarded-For", "6.6.6.6")
	if ip := clientIP(r); ip != "203.0.113.9" {
		t.Errorf("direct: got %q", ip)
	}

	// Via local proxy: last XFF entry (Caddy-appended) wins, spoofed
	// client-supplied entries are ignored.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "127.0.0.1:555"
	r2.Header.Set("X-Forwarded-For", "6.6.6.6, 198.51.100.7")
	if ip := clientIP(r2); ip != "198.51.100.7" {
		t.Errorf("proxied: got %q", ip)
	}
}
