// Package webhook — Phase 5.2 outbound webhook delivery system.
//
// The Service interface is the only entry point for HTTP handlers and
// service-layer emitters. The Worker is a separate goroutine-driven
// poller that calls DeliverDue repeatedly.
//
// HMAC signing is done here (not in the worker) so the same code path
// can be reused by future direct-delivery features (e.g. CLI replay).
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/webhook"
	"github.com/trading-journal/api/internal/store"
)

// Service is the public contract.
type Service interface {
	// Admin (JWT)
	Create(ctx context.Context, in CreateInput) (*CreateResult, error)
	List(ctx context.Context, tenantID uuid.UUID) ([]*webhook.Subscription, error)
	Get(ctx context.Context, tenantID, id uuid.UUID) (*webhook.Subscription, error)
	Update(ctx context.Context, s *webhook.Subscription) error
	Revoke(ctx context.Context, tenantID, id uuid.UUID) error

	// Emitter — called from anywhere. Best-effort: errors are returned
	// to the caller but the caller shouldn't fail user operations on them.
	Emit(ctx context.Context, in EmitInput) error

	// Worker entry — pick due deliveries, deliver them, return how many
	// were processed. The caller (WebhookWorker) re-invokes on its tick.
	DeliverDue(ctx context.Context, batchSize int, httpClient HTTPDoer) (DeliveredSummary, error)
}

// CreateInput is the input to Create.
type CreateInput struct {
	TenantID        uuid.UUID
	CreatedByUserID uuid.UUID
	URL             string
	EventTypes      []webhook.EventType
	Description     string
}

// CreateResult is what Create returns. Plaintext is shown ONCE.
type CreateResult struct {
	Subscription *webhook.Subscription `json:"subscription"`
	Plaintext    string                `json:"plaintext"` // trj_whsec_<64-hex>
}

// EmitInput is the input to Emit.
type EmitInput struct {
	TenantID  uuid.UUID
	EventType webhook.EventType
	Data      any // serialized to JSON
}

// HTTPDoer is the subset of *http.Client used by DeliverDue. Tests can
// pass a fake without spinning up an HTTP server.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// DeliveredSummary is a per-tick summary returned to the worker.
type DeliveredSummary struct {
	Picked     int
	Succeeded  int
	Failed     int
	Exhausted  int
	Rescheduled int
}

// ── Errors ───────────────────────────────────────────────────────────

var (
	ErrInvalidURL     = errors.New("webhook: invalid url")
	ErrInvalidEvent   = errors.New("webhook: invalid event type")
	ErrEmptyEvents    = errors.New("webhook: at least one event type required")
	ErrInvalidSecret  = errors.New("webhook: invalid secret")
)

// ── ServiceImpl ──────────────────────────────────────────────────────

// ServiceImpl is the production implementation. It depends on the store
// for persistence; HTTP/transport are passed in at DeliverDue time.
type ServiceImpl struct {
	store  *store.Store
	// how big a batch the worker picks per tick
	batchSize int
}

func NewServiceImpl(st *store.Store) *ServiceImpl {
	return &ServiceImpl{store: st, batchSize: 50}
}

// Create mints the secret, persists the subscription, returns the
// plaintext secret (shown ONCE).
func (s *ServiceImpl) Create(ctx context.Context, in CreateInput) (*CreateResult, error) {
	if in.URL == "" {
		return nil, ErrInvalidURL
	}
	if len(in.EventTypes) == 0 {
		return nil, ErrEmptyEvents
	}
	for _, e := range in.EventTypes {
		if !e.IsValid() {
			return nil, fmt.Errorf("%w: %s", ErrInvalidEvent, e)
		}
	}
	plaintext, err := randomSecret(32)
	if err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}
	now := time.Now().UTC()
	sub := &webhook.Subscription{
		ID:              uuid.Must(uuid.NewV7()),
		TenantID:        in.TenantID,
		URL:             in.URL,
		EventTypes:      in.EventTypes,
		Secret:          plaintext,
		Description:     in.Description,
		IsActive:        true,
		CreatedByUserID: in.CreatedByUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.store.WebhookSubs.Create(ctx, sub); err != nil {
		return nil, err
	}
	return &CreateResult{Subscription: sub, Plaintext: plaintext}, nil
}

func (s *ServiceImpl) List(ctx context.Context, tenantID uuid.UUID) ([]*webhook.Subscription, error) {
	return s.store.WebhookSubs.ListByTenant(ctx, tenantID)
}

func (s *ServiceImpl) Get(ctx context.Context, tenantID, id uuid.UUID) (*webhook.Subscription, error) {
	return s.store.WebhookSubs.GetByIDByTenant(ctx, tenantID, id)
}

func (s *ServiceImpl) Update(ctx context.Context, sub *webhook.Subscription) error {
	sub.UpdatedAt = time.Now().UTC()
	return s.store.WebhookSubs.Update(ctx, sub)
}

func (s *ServiceImpl) Revoke(ctx context.Context, tenantID, id uuid.UUID) error {
	return s.store.WebhookSubs.Revoke(ctx, tenantID, id, time.Now().UTC())
}

// Emit looks up all active subscriptions for the tenant that match the
// event type and inserts a delivery row (status=pending, next_attempt_at=now).
func (s *ServiceImpl) Emit(ctx context.Context, in EmitInput) error {
	if !in.EventType.IsValid() {
		return fmt.Errorf("%w: %s", ErrInvalidEvent, in.EventType)
	}
	subs, err := s.store.WebhookSubs.ListActiveByEvent(ctx, in.TenantID, in.EventType)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return nil
	}
	// Build payload once, reuse for all subs.
	payload, err := buildPayload(in.EventType, in.Data)
	if err != nil {
		return err
	}
	eventID := uuid.Must(uuid.NewV7()).String()
	now := time.Now().UTC()
	for _, sub := range subs {
		d := &webhook.Delivery{
			ID:             uuid.Must(uuid.NewV7()),
			TenantID:       in.TenantID,
			SubscriptionID: sub.ID,
			EventType:      in.EventType,
			EventID:        eventID,
			Payload:        payload,
			Attempt:        0,
			MaxAttempts:    5,
			Status:         webhook.DeliveryStatusPending,
			NextAttemptAt:  now,
			CreatedAt:      now,
		}
		if err := s.store.WebhookDeliveries.Create(ctx, d); err != nil {
			// Best-effort — log and continue. Returning here would lose
			// the deliveries we already inserted.
			continue
		}
	}
	return nil
}

// DeliverDue picks up to batchSize pending rows, delivers each, updates
// the row based on the receiver response.
func (s *ServiceImpl) DeliverDue(ctx context.Context, batchSize int, httpClient HTTPDoer) (DeliveredSummary, error) {
	if batchSize <= 0 {
		batchSize = s.batchSize
	}
	rows, err := s.store.WebhookDeliveries.PickDue(ctx, batchSize)
	if err != nil {
		return DeliveredSummary{}, err
	}
	summary := DeliveredSummary{Picked: len(rows)}
	for _, d := range rows {
		sub, err := s.store.WebhookSubs.GetByIDByTenant(ctx, d.TenantID, d.SubscriptionID)
		if err != nil || !sub.IsActive || sub.RevokedAt != nil {
			// Sub was revoked/deleted between Emit and Deliver — mark as failed.
			_ = s.store.WebhookDeliveries.MarkNonRetryableFailed(ctx, d.ID, 0, "subscription inactive")
			summary.Failed++
			continue
		}
		status, code, body, transportErr := deliverHTTP(ctx, httpClient, sub, d)
		switch status {
		case outcomeSuccess:
			_ = s.store.WebhookDeliveries.MarkSuccess(ctx, d.ID, code, body)
			summary.Succeeded++
		case outcomeNonRetryable:
			_ = s.store.WebhookDeliveries.MarkNonRetryableFailed(ctx, d.ID, code, body)
			summary.Failed++
		case outcomeRetryable:
			nextAttempt := d.Attempt + 1
			wait, ok := webhook.NextBackoff(nextAttempt)
			if !ok || nextAttempt >= d.MaxAttempts {
				_ = s.store.WebhookDeliveries.MarkExhausted(ctx, d.ID, &code, &body, transportErr.Code)
				summary.Exhausted++
			} else {
				next := time.Now().UTC().Add(wait)
				_ = s.store.WebhookDeliveries.MarkRetryableFailed(ctx, d.ID, nextAttempt, next, &code, &body, transportErr.Code)
				summary.Rescheduled++
			}
		}
	}
	return summary, nil
}

// ── Delivery (HTTP + classification) ──────────────────────────────────

type outcome int

const (
	outcomeSuccess outcome = iota
	outcomeNonRetryable
	outcomeRetryable
)

type transportErr struct {
	Code string // e.g. "timeout", "tls_error", "dns_error"
}

func deliverHTTP(ctx context.Context, client HTTPDoer, sub *webhook.Subscription, d *webhook.Delivery) (outcome, int, string, transportErr) {
	ts := time.Now().Unix()
	body := d.Payload
	sig := SignPayload(sub.Secret, ts, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.URL, bytesReader(body))
	if err != nil {
		return outcomeNonRetryable, 0, "build request", transportErr{"invalid_url"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Trading-Journal-Webhooks/1.0")
	req.Header.Set("X-Webhook-Id", d.ID.String())
	req.Header.Set("X-Webhook-Event", string(d.EventType))
	req.Header.Set("X-Webhook-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Webhook-Signature", sig)
	req.Header.Set("X-Webhook-Attempt", strconv.Itoa(d.Attempt+1))

	res, err := client.Do(req)
	if err != nil {
		// Network error — treat as retryable.
		return outcomeRetryable, 0, "transport: " + err.Error(), transportErr{"network_error"}
	}
	defer res.Body.Close()
	bodyBytes := make([]byte, 0, 1024)
	buf := make([]byte, 1024)
	for {
		n, err := res.Body.Read(buf)
		if n > 0 {
			bodyBytes = append(bodyBytes, buf[:n]...)
			if len(bodyBytes) >= 4096 {
				break
			}
		}
		if err != nil {
			break
		}
	}
	bodyStr := string(bodyBytes)
	if webhook.IsRetryableResponse(res.StatusCode) {
		return outcomeRetryable, res.StatusCode, bodyStr, transportErr{"retryable_status"}
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return outcomeSuccess, res.StatusCode, bodyStr, transportErr{}
	}
	return outcomeNonRetryable, res.StatusCode, bodyStr, transportErr{"client_error"}
}

// ── HMAC signing ─────────────────────────────────────────────────────

// SignPayload computes hex(HMAC-SHA256(secret, timestamp + "." + body)).
// The timestamp binding prevents replay attacks; the receiver should
// verify the timestamp is within a 5-minute window.
func SignPayload(secret string, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte{'.'})
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifyPayload re-computes the signature and compares in constant time.
// Returns true if it does. Used by external clients; included here so
// the wire format is co-located with the producer.
func VerifyPayload(secret string, ts int64, body []byte, givenSig string) bool {
	want := SignPayload(secret, ts, body)
	return hmac.Equal([]byte(want), []byte(givenSig))
}

// ── helpers ──────────────────────────────────────────────────────────

func buildPayload(eventType webhook.EventType, data any) ([]byte, error) {
	envelope := map[string]any{
		"event": map[string]any{
			"type":       string(eventType),
			"id":         uuid.Must(uuid.NewV7()).String(),
			"created_at": time.Now().UTC().Format(time.RFC3339),
		},
		"data": data,
	}
	return json.Marshal(envelope)
}

// bytesReader is a tiny helper to avoid pulling bytes.NewReader in the
// signature (keeps the import block short).
func bytesReader(b []byte) *bytes.Reader {
	return bytes.NewReader(b)
}