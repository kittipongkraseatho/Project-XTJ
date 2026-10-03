package mailer

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SMTPMailer sends mail via a real SMTP server (Gmail, SendGrid, SES, etc.).
// Phase 4 minimum: enough to send a no-attachment text mail. Attachments
// are MIME-encoded; Phase 5 may swap for a richer library.
type SMTPMailer struct {
	host     string
	port     int
	username string
	password string
	from     string
}

// SMTPMailerConfig is the wire-up for SMTPMailer.
type SMTPMailerConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // RFC 5322 From address
}

// NewSMTPMailer returns a configured SMTPMailer.
func NewSMTPMailer(cfg SMTPMailerConfig) (*SMTPMailer, error) {
	if cfg.Host == "" || cfg.Port == 0 {
		return nil, fmt.Errorf("smtp: host + port required")
	}
	return &SMTPMailer{
		host:     cfg.Host,
		port:     cfg.Port,
		username: cfg.Username,
		password: cfg.Password,
		from:     cfg.From,
	}, nil
}

// Send delivers msg via SMTP. Uses smtp.SendMail over a fresh connection.
// Per-request connections are fine for low-traffic scheduled reports;
// switch to a connection pool if volume grows.
func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	addr := net.JoinHostPort(m.host, fmt.Sprintf("%d", m.port))

	body := renderSmtpBody(msg)

	var auth smtp.Auth
	if m.username != "" {
		auth = smtp.PlainAuth("", m.username, m.password, m.host)
	}

	// Honor ctx (smtp.SendMail has no context support).
	errCh := make(chan error, 1)
	go func() {
		errCh <- smtp.SendMail(addr, auth, m.from, msg.To, []byte(body))
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("smtp send: %w", err)
		}
		return nil
	case <-time.After(30 * time.Second):
		return fmt.Errorf("smtp send: timeout after 30s")
	}
}

// renderSmtpBody builds a minimal multipart/mixed message. Plain-text body
// only — full MIME encoding for attachments can be added in Phase 5.
func renderSmtpBody(msg Message) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("From: %s\r\n", msg.From))
	b.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(msg.To, ", ")))
	b.WriteString(fmt.Sprintf("Subject: %s\r\n", msg.Subject))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/mixed; boundary=\"tj-bd\"\r\n")
	b.WriteString("\r\n--tj-bd\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(msg.TextBody)
	b.WriteString("\r\n--tj-bd--\r\n")
	return b.String()
}