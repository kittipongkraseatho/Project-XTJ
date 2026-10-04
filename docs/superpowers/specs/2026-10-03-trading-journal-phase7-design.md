# Trading Journal — Phase 7 Design

| | |
|---|---|
| **Status** | ✅ Approved 2026-10-04 — implementation starting |
| **Date** | 2026-10-04 |
| **Phase** | 7 — Stripe Webhook State Machine |
| **Scope** | Apply 5 webhook event types to subscriptions + payments tables · Idempotency on event id · Stub + Stripe impls · Tests |
| **Out of scope** | Real-time Stripe API sync · Subscription-level plan upgrades from API · Refunds (Phase 8+) · Stripe Connect |

---

## 1. Decisions Resolved

| Fork | Decision |
|---|---|
| Events to handle | **5**: `checkout.session.completed`, `customer.subscription.updated`, `customer.subscription.deleted`, `invoice.paid`, `invoice.payment_failed` |
| Idempotency strategy | **`stripe_event_id` UNIQUE** column on `webhook_events` table; duplicate inserts return 200 (no-op) |
| Failure mode | Best-effort: log error + return 200 to Stripe (so they don't retry forever); 5xx for transient failures only |
| Webhook ordering | Out of order is possible but rare; latest timestamp wins via Stripe's `created` field |
| Mapping tenant to subscription | Add `stripe_subscription_id` UNIQUE column to `subscriptions`; new `stripe_event_id` UNIQUE on `webhook_events` |

---

## 2. New column (Migration 0009)

### 2.1 `subscriptions` table additions

| Column | Type | Notes |
|---|---|---|
| `stripe_customer_id` | TEXT | from Stripe customer object |
| `stripe_subscription_id` | TEXT UNIQUE | from Stripe subscription object |
| `stripe_event_id` | TEXT | last processed event id (for ordering audit) |

Indexes:
- INDEX(`stripe_customer_id`)
- UNIQUE(`stripe_subscription_id`) — already implicit if the new column has UNIQUE

### 2.2 `payments` table additions

| Column | Type | Notes |
|---|---|---|
| `stripe_invoice_id` | TEXT UNIQUE | dedup key from Stripe |
| `stripe_charge_id` | TEXT | nullable |

### 2.3 `webhook_events` table (new — idempotency log)

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | |
| `stripe_event_id` | TEXT NOT NULL UNIQUE | from `event.id` (e.g. `evt_1A2B3C`) |
| `event_type` | TEXT NOT NULL | |
| `received_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Indexes:
- UNIQUE(`stripe_event_id`)

---

## 3. Service interface change

```go
// internal/integration/billing/service.go

type Service interface {
    // ...existing methods (CreateCheckout, Portal, CurrentSubscription, etc.)

    // Webhook-side
    VerifyWebhookSignature(payload []byte, signatureHeader string) (*WebhookEvent, error)

    // NEW: apply the parsed event to local state.
    // Idempotent — duplicate event_id returns nil without touching state.
    // Failure path: returns error → caller logs + returns 500 (Stripe retries).
    HandleWebhookEvent(ctx context.Context, ev *WebhookEvent) error
}
```

`WebhookEvent` already exists in Phase 4 (from `VerifyWebhookSignature`); we'll extend it with the raw event data needed for handlers.

---

## 4. Event handlers

### 4.1 `checkout.session.completed`

```json
{
  "id": "evt_1A2B3C",
  "type": "checkout.session.completed",
  "data": {
    "object": {
      "id": "cs_test_...",
      "customer": "cus_...",
      "subscription": "sub_...",
      "client_reference_id": "<tenant_id>",
      "metadata": { "tenant_id": "...", "plan_code": "pro" },
      "amount_total": 19900,
      "currency": "thb",
      "status": "complete"
    }
  }
}
```

Behavior:
1. Extract `tenant_id` from `client_reference_id` (preferred) or `metadata.tenant_id`
2. Extract `plan_code` from `metadata.plan_code`
3. UPSERT `subscriptions` row:
   - `tenant_id` = extracted
   - `plan_code` = extracted
   - `status` = `active`
   - `stripe_customer_id`, `stripe_subscription_id` set
   - `current_period_start` / `end` — not in this event; use now() / now()+1 month for now
   - `external_subscription_id` (legacy column) = stripe_subscription_id

### 4.2 `customer.subscription.updated`

```json
{
  "id": "evt_...",
  "type": "customer.subscription.updated",
  "data": {
    "object": {
      "id": "sub_...",
      "customer": "cus_...",
      "status": "active" | "past_due" | "canceled" | "trialing" | ...,
      "current_period_start": <unix>,
      "current_period_end": <unix>,
      "cancel_at_period_end": false,
      "items": [{"price": {"lookup_key": "pro"}, ...}]
    }
  }
}
```

Behavior:
1. Find local subscription by `stripe_subscription_id`
2. Update `status`, `current_period_start`, `current_period_end`, `cancel_at_period_end`
3. Re-derive `plan_code` from `items[0].price.lookup_key` (Phase 4 plan code)

### 4.3 `customer.subscription.deleted`

Behavior:
1. Find subscription by `stripe_subscription_id`
2. Set `status = canceled`, `canceled_at = now()`

### 4.4 `invoice.paid`

```json
{
  "id": "evt_...",
  "type": "invoice.paid",
  "data": {
    "object": {
      "id": "in_...",
      "subscription": "sub_...",
      "customer": "cus_...",
      "amount_paid": 19900,
      "currency": "thb",
      "status": "paid",
      "period_start": <unix>,
      "period_end": <unix>,
      "charge": "ch_..." (nullable)
    }
  }
}
```

Behavior:
1. Find local subscription by `stripe_subscription_id` → get tenant_id
2. UPSERT `payments` row:
   - `tenant_id`, `amount_cents`, `currency`, `status = paid`, `paid_at = now()`
   - `stripe_invoice_id` = `in_...` (UNIQUE — dedup)
   - `stripe_charge_id` = `ch_...`

### 4.5 `invoice.payment_failed`

Behavior:
1. Find subscription by `stripe_subscription_id` → set `status = past_due`
2. UPSERT `payments` row with `status = failed` + error from Stripe

---

## 5. Stub service parity

`StubBillingService` implements `HandleWebhookEvent` as a no-op (returns nil). This keeps local dev simple — the `/v1/billing/webhook` endpoint stays verification-only for stub mode, which is the expected dev behavior.

Production (Stripe enabled) bills HandleWebhookEvent is the real implementation.

---

## 6. Webhook HTTP handler update

Currently the handler just verifies the signature. Update it to also apply:

```go
func (h *Handler) Webhook(c echo.Context) error {
    body := readBody(...)
    sig := c.Request().Header.Get("Stripe-Signature")
    if sig == "" { return 400 }

    ev, err := h.billing.VerifyWebhookSignature(body, sig)
    if err != nil { return 400 }

    // NEW: apply the event
    if err := h.billing.HandleWebhookEvent(c.Request().Context(), ev); err != nil {
        h.log.Warn("webhook apply failed", slog.String("event_id", ev.ID), slog.String("err", err.Error()))
        // Return 500 so Stripe retries; idempotency layer prevents double-apply
        return c.JSON(http.StatusInternalServerError, ...)
    }
    return c.NoContent(http.StatusNoContent)
}
```

---

## 7. Tests

### 7.1 Unit tests (real `ServiceImpl`)

For each event type:
- Build a fake `*WebhookEvent` with realistic shape
- Call `HandleWebhookEvent`
- Assert the local state changed (using a stub repo or in-memory store)

### 7.2 Idempotency test

Send the same event twice → assert second call is a no-op (no row duplication).

### 7.3 Signature verification unchanged

Existing Phase 4 signature tests stay green.

---

## 8. Definition of Done — Phase 7

- [ ] Migration 0009 applied; new columns + `webhook_events` table exist
- [ ] `billing.Service` interface gains `HandleWebhookEvent`
- [ ] `StripeBillingService.HandleWebhookEvent` handles 5 event types
- [ ] `StubBillingService.HandleWebhookEvent` is a no-op
- [ ] Webhook handler calls `HandleWebhookEvent` after signature verify
- [ ] Idempotency: duplicate event_id doesn't double-apply (UNIQUE on `webhook_events.stripe_event_id`)
- [ ] Unit tests for all 5 event types + idempotency (≥ 8 tests, all green)
- [ ] `go build ./...` + `go test -race -short ./...` green
- [ ] AGENTS.md: Phase 7 complete; "Adding a new Stripe event" guide
- [ ] .env.example: `STRIPE_WEBHOOK_TOLERANCE_SECONDS=300` (new)

---

## 9. Implementation Order

1. **Design doc** (this file)
2. **Migration 0009** + store column additions
3. **WebhookEvent type extension** (raw payload + event_type + event_id fields)
4. **Service.HandleWebhookEvent** (5 handlers + idempotency)
5. **Webhook HTTP handler** updated to call HandleWebhookEvent
6. **Unit tests** for each event type + idempotency
7. **AGENTS.md + .env.example** + final test run

---

## 10. Out of Scope (Phase 7)

- Real-time Stripe API sync (Phase 7+)
- Subscription-level plan upgrades from API (Phase 8)
- Refunds / partial refunds (Phase 8+)
- Stripe Connect (multi-party payments — never planned)
- Audit log of every webhook apply (Phase 8+)