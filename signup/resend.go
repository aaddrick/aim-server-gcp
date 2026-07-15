package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// resendClient sends transactional email through the Resend HTTP API.
// Plain net/http — no SDK dependency.
type resendClient struct {
	apiKey  string
	from    string
	baseURL string // overridable in tests
	client  *http.Client
}

func newResendClient(apiKey, from string) *resendClient {
	return &resendClient{
		apiKey:  apiKey,
		from:    from,
		baseURL: "https://api.resend.com",
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

type resendEmail struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	Text    string   `json:"text"`
}

func (r *resendClient) send(to, subject, htmlBody, textBody string) error {
	payload, err := json.Marshal(resendEmail{
		From:    r.from,
		To:      []string{to},
		Subject: subject,
		HTML:    htmlBody,
		Text:    textBody,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, r.baseURL+"/emails", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("resend API returned %d: %s", resp.StatusCode, body)
	}
	return nil
}
