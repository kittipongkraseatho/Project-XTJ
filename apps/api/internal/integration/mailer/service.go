// Package mailer is the email-sending abstraction.
//
// Phase 4 ships two implementations:
//
//   - StubMailer   — drops .eml files into a local directory (dev / tests)
//   - SMTPMailer   — sends via net/smtp (production)
//
// Add a new backend (SendGrid, Mailgun, SES) by implementing Service +
// wiring it in the constructor.
package mailer

import "context"

// Service is the contract every mailer backend implements.
type Service interface {
	// Send delivers a single email with optional attachments. Backends MUST
	// honor ctx for cancellation. Errors are non-retryable by default;
	// the caller (ScheduleWorker) will retry on the next poll.
	Send(ctx context.Context, msg Message) error
}

// Attachment is a single file attached to the email.
type Attachment struct {
	Filename string
	MimeType string // e.g. "application/pdf"
	Data     []byte
}

// Message is the unit of work for Send.
type Message struct {
	From        string
	To          []string
	Subject     string
	TextBody    string // plain-text fallback (always required)
	HTMLBody    string // optional HTML alternative
	Attachments []Attachment
}