package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type app struct {
	baseURL    string // public URL of this service, e.g. https://aim.example.com
	serverHost string // hostname AIM clients connect to (derived from baseURL)
	mgmtAPI    string // open-oscar-server management API, e.g. http://127.0.0.1:8080
	tokenTTL   time.Duration
	emailCap   int

	store   *store
	limiter *rateLimiter
	mailer  *resendClient
	client  *http.Client
	logger  *slog.Logger
}

func (a *app) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.handleForm)
	mux.HandleFunc("POST /signup", a.handleSignup)
	mux.HandleFunc("GET /verify", a.handleVerify)
	mux.HandleFunc("GET /reset", a.handleResetForm)
	mux.HandleFunc("POST /reset", a.handleResetRequest)
	mux.HandleFunc("GET /reset/confirm", a.handleResetConfirmForm)
	mux.HandleFunc("POST /reset/confirm", a.handleResetConfirm)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok\n")
	})
	return mux
}

func (a *app) render(w http.ResponseWriter, status int, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := pageTmpl.Execute(w, p); err != nil {
		a.logger.Error("template render failed", "err", err)
	}
}

func (a *app) handleForm(w http.ResponseWriter, r *http.Request) {
	a.render(w, http.StatusOK, page{Title: "Get an AIM Screen Name", ShowForm: true})
}

func (a *app) handleSignup(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.limiter.allow(ip) {
		a.render(w, http.StatusTooManyRequests, page{
			Title: "Whoa there",
			Error: "Too many signup attempts from your address. Wait ten minutes and try again.",
		})
		return
	}

	if err := r.ParseForm(); err != nil {
		a.render(w, http.StatusBadRequest, page{Title: "Get an AIM Screen Name", ShowForm: true, Error: "That form didn't parse. Try again."})
		return
	}

	screenName := strings.TrimSpace(r.PostFormValue("screen_name"))
	email := strings.TrimSpace(r.PostFormValue("email"))

	// Honeypot: bots fill every field. Pretend success, create nothing.
	if r.PostFormValue("website") != "" {
		a.logger.Info("honeypot tripped", "ip", ip)
		a.render(w, http.StatusOK, page{Title: "Check Your Email", Message: "Almost done! Check your inbox for a confirmation link."})
		return
	}

	formErr := func(msg string) {
		a.render(w, http.StatusUnprocessableEntity, page{
			Title: "Get an AIM Screen Name", ShowForm: true, Error: msg,
			ScreenName: screenName, Email: email,
		})
	}

	if err := validateScreenName(screenName); err != nil {
		formErr(err.Error())
		return
	}
	cleanEmail, err := validateEmail(email)
	if err != nil {
		formErr(err.Error())
		return
	}

	taken, err := a.screenNameTaken(r, screenName)
	if err != nil {
		a.logger.Error("management API availability check failed", "err", err)
		a.render(w, http.StatusBadGateway, page{Title: "Server Trouble", Error: "Couldn't talk to the AIM server. Try again in a minute."})
		return
	}
	if taken {
		formErr(fmt.Sprintf("Screen name %q is taken. Add some numbers like it's 1999.", screenName))
		return
	}

	if err := a.store.countEmail(a.emailCap); err != nil {
		a.render(w, http.StatusServiceUnavailable, page{Title: "Signups Paused", Error: err.Error()})
		return
	}

	token, err := a.store.add(pendingSignup{ScreenName: screenName, Email: cleanEmail})
	if errors.Is(err, errPendingExists) {
		formErr("A signup for that screen name is already waiting on email verification. Check your inbox (and spam folder).")
		return
	}
	if err != nil {
		a.logger.Error("storing pending signup failed", "err", err)
		a.render(w, http.StatusInternalServerError, page{Title: "Server Trouble", Error: "Something broke on our end. Try again."})
		return
	}

	link := a.baseURL + "/verify?token=" + url.QueryEscape(token)
	ttl := a.tokenTTL.String()
	err = a.mailer.send(cleanEmail,
		fmt.Sprintf("Confirm your AIM screen name %q", screenName),
		fmt.Sprintf(verifyEmailHTML, screenName, a.serverHost, link, ttl),
		fmt.Sprintf(verifyEmailText, screenName, a.serverHost, ttl, link),
	)
	if err != nil {
		a.store.remove(token) // let them retry immediately
		a.logger.Error("verification email failed", "err", err, "ip", ip)
		a.render(w, http.StatusBadGateway, page{Title: "Email Trouble", Error: "Couldn't send the verification email. Try again in a minute."})
		return
	}

	a.logger.Info("verification email sent", "screen_name", screenName, "ip", ip)
	a.render(w, http.StatusOK, page{
		Title:   "Check Your Email",
		Message: fmt.Sprintf("Almost done! We sent a confirmation link to %s. Click it within %s to activate %q.", cleanEmail, ttl, screenName),
	})
}

func (a *app) handleVerify(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.limiter.allow(ip) {
		a.render(w, http.StatusTooManyRequests, page{Title: "Whoa there", Error: "Too many attempts. Wait ten minutes and try again."})
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		a.render(w, http.StatusBadRequest, page{Title: "Invalid Link", Error: errTokenInvalid.Error()})
		return
	}

	p, err := a.store.take(token)
	if err != nil {
		a.render(w, http.StatusNotFound, page{Title: "Invalid Link", Error: errTokenInvalid.Error()})
		return
	}

	// The password exists only here and on the success page the user is
	// about to see. It is never persisted or emailed.
	password, err := generatePassword()
	if err != nil {
		a.logger.Error("password generation failed", "err", err)
		a.render(w, http.StatusInternalServerError, page{Title: "Server Trouble", Error: "Something broke on our end. Your link has been used up — sign up again."})
		return
	}

	status, err := a.createUser(r, p.ScreenName, password)
	switch {
	case err != nil:
		a.logger.Error("management API create user failed", "err", err)
		a.render(w, http.StatusBadGateway, page{Title: "Server Trouble", Error: "Couldn't reach the AIM server to create your account. Your link has been used up — sign up again."})
		return
	case status == http.StatusConflict:
		a.render(w, http.StatusConflict, page{Title: "Screen Name Taken", Error: fmt.Sprintf("Someone claimed %q while your email was in flight. Sign up again with a different name.", p.ScreenName)})
		return
	case status != http.StatusCreated:
		a.logger.Error("management API rejected user", "status", status, "screen_name", p.ScreenName)
		a.render(w, http.StatusBadGateway, page{Title: "Server Trouble", Error: "The AIM server rejected the account. Sign up again."})
		return
	}

	// Remember which email verified this account so password resets can
	// go to an address the owner provably held.
	if err := a.store.recordAccount(identScreenName(p.ScreenName), p.Email); err != nil {
		a.logger.Error("recording verified email failed", "err", err, "screen_name", p.ScreenName)
	}

	a.logger.Info("account created", "screen_name", p.ScreenName)
	a.render(w, http.StatusOK, page{
		Title:      "Welcome to AIM",
		Verified:   true,
		ScreenName: p.ScreenName,
		Password:   password,
		Host:       a.serverHost,
	})
}

func (a *app) handleResetForm(w http.ResponseWriter, r *http.Request) {
	a.render(w, http.StatusOK, page{Title: "Reset Your Password", ShowResetForm: true})
}

func (a *app) handleResetRequest(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.limiter.allow(ip) {
		a.render(w, http.StatusTooManyRequests, page{Title: "Whoa there", Error: "Too many attempts. Wait ten minutes and try again."})
		return
	}

	if err := r.ParseForm(); err != nil {
		a.render(w, http.StatusBadRequest, page{Title: "Reset Your Password", ShowResetForm: true, Error: "That form didn't parse. Try again."})
		return
	}

	screenName := strings.TrimSpace(r.PostFormValue("screen_name"))

	// The response never says whether the account or an email on file
	// exists — same page either way.
	neutral := func() {
		a.render(w, http.StatusOK, page{
			Title:   "Check Your Email",
			Message: fmt.Sprintf("If %q was registered here, a reset link is on its way to the email that verified it. The link expires in %s.", screenName, resetTTL.String()),
		})
	}

	// Honeypot: same treatment as signup.
	if r.PostFormValue("website") != "" {
		a.logger.Info("honeypot tripped on reset", "ip", ip)
		neutral()
		return
	}

	if err := validateScreenName(screenName); err != nil {
		a.render(w, http.StatusUnprocessableEntity, page{Title: "Reset Your Password", ShowResetForm: true, Error: err.Error(), ScreenName: screenName})
		return
	}

	email, ok := a.store.emailFor(identScreenName(screenName))
	if !ok {
		// Accounts that predate the signup service (or were created by
		// hand) have no verified email on file; they can still change
		// their password from inside the client.
		a.logger.Info("reset requested for account with no email on file", "screen_name", screenName, "ip", ip)
		neutral()
		return
	}

	if err := a.store.countEmail(a.emailCap); err != nil {
		a.render(w, http.StatusServiceUnavailable, page{Title: "Resets Paused", Error: err.Error()})
		return
	}

	token, err := a.store.addReset(screenName)
	if err != nil {
		a.logger.Error("storing pending reset failed", "err", err)
		a.render(w, http.StatusInternalServerError, page{Title: "Server Trouble", Error: "Something broke on our end. Try again."})
		return
	}

	link := a.baseURL + "/reset/confirm?token=" + url.QueryEscape(token)
	err = a.mailer.send(email,
		fmt.Sprintf("Reset the password for AIM screen name %q", screenName),
		fmt.Sprintf(resetEmailHTML, screenName, a.serverHost, link, resetTTL.String()),
		fmt.Sprintf(resetEmailText, screenName, a.serverHost, resetTTL.String(), link),
	)
	if err != nil {
		a.store.removeReset(token) // let them retry immediately
		a.logger.Error("reset email failed", "err", err, "ip", ip)
		a.render(w, http.StatusBadGateway, page{Title: "Email Trouble", Error: "Couldn't send the reset email. Try again in a minute."})
		return
	}

	a.logger.Info("reset email sent", "screen_name", screenName, "ip", ip)
	neutral()
}

func (a *app) handleResetConfirmForm(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if _, err := a.store.peekReset(token); err != nil {
		a.render(w, http.StatusNotFound, page{Title: "Invalid Link", Error: errTokenInvalid.Error()})
		return
	}
	a.render(w, http.StatusOK, page{Title: "Choose a New Password", ShowResetConfirm: true, Token: token})
}

func (a *app) handleResetConfirm(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !a.limiter.allow(ip) {
		a.render(w, http.StatusTooManyRequests, page{Title: "Whoa there", Error: "Too many attempts. Wait ten minutes and try again."})
		return
	}

	if err := r.ParseForm(); err != nil {
		a.render(w, http.StatusBadRequest, page{Title: "Invalid Link", Error: errTokenInvalid.Error()})
		return
	}

	token := r.PostFormValue("token")
	password := r.PostFormValue("password")

	// Validate before consuming the token so a typo doesn't burn the link.
	if err := validatePassword(password); err != nil {
		if _, peekErr := a.store.peekReset(token); peekErr != nil {
			a.render(w, http.StatusNotFound, page{Title: "Invalid Link", Error: errTokenInvalid.Error()})
			return
		}
		a.render(w, http.StatusUnprocessableEntity, page{Title: "Choose a New Password", ShowResetConfirm: true, Token: token, Error: err.Error()})
		return
	}

	reset, err := a.store.takeReset(token)
	if err != nil {
		a.render(w, http.StatusNotFound, page{Title: "Invalid Link", Error: errTokenInvalid.Error()})
		return
	}

	status, err := a.setPassword(r, reset.ScreenName, password)
	switch {
	case err != nil:
		a.logger.Error("management API set password failed", "err", err)
		a.render(w, http.StatusBadGateway, page{Title: "Server Trouble", Error: "Couldn't reach the AIM server. Your link has been used up — request a new reset."})
		return
	case status == http.StatusNotFound:
		a.render(w, http.StatusNotFound, page{Title: "Account Gone", Error: fmt.Sprintf("The account %q no longer exists.", reset.ScreenName)})
		return
	case status != http.StatusNoContent:
		a.logger.Error("management API rejected password", "status", status, "screen_name", reset.ScreenName)
		a.render(w, http.StatusBadGateway, page{Title: "Server Trouble", Error: "The AIM server rejected the new password. Request a new reset."})
		return
	}

	a.logger.Info("password reset", "screen_name", reset.ScreenName)
	a.render(w, http.StatusOK, page{
		Title:   "Password Changed",
		Message: fmt.Sprintf("All set. Sign on as %q with your new password.", reset.ScreenName),
	})
}

// screenNameTaken asks the management API whether a screen name exists.
func (a *app) screenNameTaken(r *http.Request, screenName string) (bool, error) {
	u := fmt.Sprintf("%s/user/%s/account", a.mgmtAPI, url.PathEscape(screenName))
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected status %d from management API", resp.StatusCode)
	}
}

// createUser registers the account via the management API and returns the
// HTTP status (201 created, 409 duplicate, 400 invalid).
func (a *app) createUser(r *http.Request, screenName, password string) (int, error) {
	return a.userRequest(r, http.MethodPost, "/user", screenName, password)
}

// setPassword replaces an account's password via the management API and
// returns the HTTP status (204 changed, 404 no such user).
func (a *app) setPassword(r *http.Request, screenName, password string) (int, error) {
	return a.userRequest(r, http.MethodPut, "/user/password", screenName, password)
}

func (a *app) userRequest(r *http.Request, method, path, screenName, password string) (int, error) {
	body, err := json.Marshal(map[string]string{
		"screen_name": screenName,
		"password":    password,
	})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(r.Context(), method, a.mgmtAPI+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// clientIP resolves the caller's address. The service binds to localhost
// behind Caddy, which appends the real client IP to X-Forwarded-For — so
// that header is only trusted when the direct peer is loopback, and only
// its last (proxy-appended) entry is used. Anything a client spoofs earlier
// in the list is ignored.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return host
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return host
	}
	parts := strings.Split(xff, ",")
	return strings.TrimSpace(parts[len(parts)-1])
}
