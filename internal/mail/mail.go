// Package mail sends the API's transactional emails.
package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Log "sends" by writing the message to the log. It is the default so development needs no mail account.
type Log struct{ log *slog.Logger }

// NewLog returns a mailer that only logs.
func NewLog(log *slog.Logger) *Log { return &Log{log: log} }

// Send logs the message, body included, so a developer can copy a reset link from it.
func (l *Log) Send(ctx context.Context, to, subject, body string) error {
	l.log.InfoContext(ctx, "email not sent (MAIL_API_KEY unset)", "to", to, "subject", subject, "body", body)
	return nil
}

// Resend sends through the Resend HTTP API (https://resend.com/docs/api-reference/emails/send-email).
type Resend struct {
	apiKey, from, url string
	client            *http.Client
}

// NewResend returns a Resend mailer. url is the send endpoint, e.g. https://api.resend.com/emails.
func NewResend(apiKey, from, url string) *Resend {
	return &Resend{apiKey: apiKey, from: from, url: url, client: &http.Client{Timeout: 10 * time.Second}}
}

// Send posts one plain-text email; any non-2xx answer is an error.
func (r *Resend) Send(ctx context.Context, to, subject, body string) error {
	payload, err := json.Marshal(map[string]any{"from": r.from, "to": []string{to}, "subject": subject, "text": body})
	if err != nil {
		return fmt.Errorf("encode email: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build email request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("send email: provider answered %d: %s", resp.StatusCode, msg)
	}
	return nil
}
