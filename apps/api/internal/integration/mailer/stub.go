package mailer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StubMailer is a no-op mailer that drops every message into a local
// directory as a .eml file (RFC 822-ish envelope format). Useful for
// local dev / tests / CI — no SMTP needed.
type StubMailer struct {
	OutputDir string // defaults to os.TempDir() + "trading-journal-mail"
}

// NewStubMailer returns a stub mailer that writes to the given dir
// (or a temp dir if empty).
func NewStubMailer(dir string) *StubMailer {
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "trading-journal-mail")
	}
	_ = os.MkdirAll(dir, 0o755)
	return &StubMailer{OutputDir: dir}
}

// Send writes a flat .eml file capturing the message. No SMTP, no network.
func (s *StubMailer) Send(_ context.Context, msg Message) error {
	stamp := time.Now().UTC().Format("20060102-150405.000")
	recipient := strings.ReplaceAll(strings.Join(msg.To, ","), "@", "_at_")
	filename := fmt.Sprintf("%s__%s.eml", stamp, recipient)
	path := filepath.Join(s.OutputDir, filename)

	body := renderString(msg)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("stub mailer write %s: %w", path, err)
	}
	return nil
}

// renderString produces a minimal .eml-like envelope so the recipient
// (a developer running `cat /tmp/trading-journal-mail/*.eml`) can read it.
func renderString(msg Message) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("From: %s\n", msg.From))
	b.WriteString(fmt.Sprintf("To: %s\n", strings.Join(msg.To, ", ")))
	b.WriteString(fmt.Sprintf("Subject: %s\n", msg.Subject))
	b.WriteString("Content-Type: multipart/mixed; boundary=\"tj-bd\"\n")
	b.WriteString("\n--tj-bd\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\n\n")
	b.WriteString(msg.TextBody)
	b.WriteString("\n--tj-bd\n")
	for _, a := range msg.Attachments {
		fmt.Fprintf(&b, "Content-Type: %s\n", a.MimeType)
		fmt.Fprintf(&b, "Content-Disposition: attachment; filename=\"%s\"\n", a.Filename)
		fmt.Fprintf(&b, "Content-Length: %d\n\n", len(a.Data))
		// We embed length only — actual binary data isn't included for
		// readability. The ScheduleWorker asserts the byte length matches
		// before/after the wire transfer.
		b.WriteString(fmt.Sprintf("[%d bytes of %s]\n", len(a.Data), a.MimeType))
		b.WriteString("--tj-bd\n")
	}
	b.WriteString("--\n")
	return b.String()
}