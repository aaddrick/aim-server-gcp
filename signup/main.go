// aim-signup is a small email-verification gate in front of
// open-oscar-server's management API. It serves a signup form, emails a
// verification link via Resend, and creates the account only after the
// link is clicked — with a generated password shown once, so no
// user-chosen credential is ever stored. Password resets work the same
// way: a link goes to the email that verified the account.
// Stdlib only — builds with any Go >= 1.22.
package main

import (
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	baseURL := os.Getenv("BASE_URL") // e.g. https://aim.example.com
	if baseURL == "" {
		logger.Error("BASE_URL is required (public URL of this service)")
		os.Exit(1)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Hostname() == "" {
		logger.Error("BASE_URL is not a valid URL", "value", baseURL)
		os.Exit(1)
	}

	from := os.Getenv("EMAIL_FROM") // e.g. AIM Signup <verify@aim.example.com>
	if from == "" {
		logger.Error("EMAIL_FROM is required")
		os.Exit(1)
	}

	apiKey := os.Getenv("RESEND_API_KEY")
	if keyFile := os.Getenv("RESEND_API_KEY_FILE"); apiKey == "" && keyFile != "" {
		data, err := os.ReadFile(keyFile)
		if err != nil {
			logger.Error("reading RESEND_API_KEY_FILE failed", "err", err)
			os.Exit(1)
		}
		apiKey = string(data)
	}
	if apiKey == "" {
		logger.Error("RESEND_API_KEY or RESEND_API_KEY_FILE is required")
		os.Exit(1)
	}

	tokenTTL, err := time.ParseDuration(envOr("TOKEN_TTL", "24h"))
	if err != nil {
		logger.Error("invalid TOKEN_TTL", "err", err)
		os.Exit(1)
	}

	emailCap, err := strconv.Atoi(envOr("DAILY_EMAIL_CAP", "90"))
	if err != nil {
		logger.Error("invalid DAILY_EMAIL_CAP", "err", err)
		os.Exit(1)
	}

	st, err := newStore(envOr("STATE_PATH", "/var/lib/aim-signup/state.json"), tokenTTL)
	if err != nil {
		logger.Error("opening state file failed", "err", err)
		os.Exit(1)
	}

	a := &app{
		baseURL:    baseURL,
		serverHost: parsed.Hostname(),
		mgmtAPI:    envOr("MGMT_API_URL", "http://127.0.0.1:8080"),
		tokenTTL:   tokenTTL,
		emailCap:   emailCap,
		store:      st,
		// flyspacea-tuned: 10 attempts per 10 minutes per IP, 10-minute ban.
		limiter: newRateLimiter(10, 10*time.Minute, 10*time.Minute),
		mailer:  newResendClient(apiKey, from),
		client:  &http.Client{Timeout: 10 * time.Second},
		logger:  logger,
	}

	go func() {
		for range time.Tick(time.Hour) {
			a.limiter.cleanup()
		}
	}()

	listen := envOr("SIGNUP_LISTEN", "127.0.0.1:8090")
	logger.Info("aim-signup listening", "addr", listen, "server_host", a.serverHost)
	srv := &http.Server{
		Addr:              listen,
		Handler:           a.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("server exited", "err", err)
		os.Exit(1)
	}
}
