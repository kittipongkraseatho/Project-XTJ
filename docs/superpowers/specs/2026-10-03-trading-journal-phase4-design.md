# Trading Journal — Phase 4 Design

| | |
|---|---|
| **Status** | ✅ Approved 2026-10-03 (2 forks resolved) — implementation starting |
| **Date** | 2026-10-03 |
| **Phase** | 4 — Billing (Stripe) + Real MetaApi + Email Report Scheduling |
| **Scope** | Stripe Checkout + webhooks · Plan upgrade/downgrade · Real MetaApi SDK adapter · Scheduled PDF report email delivery |
| **Out of scope (deferred)** | AI analysis · Public API + tokens (defer to Phase 5) |
| **Predecessor** | Phase 0–3 (auth, trades, broker sync, PDF reports) |

---

## 1. Decisions Resolved

| Fork | Decision |
|---|---|
| Phase 4 scope | Billing (Stripe) + Real MetaApi adapter + Email report scheduling. AI analysis + public API defer to Phase 5. |
| Billing provider | **Stripe** (international). Omise is the obvious Thai choice but Stripe's API + docs + webhooks are more battle-tested; the user can swap by writing a 1-file `Provider` adapter. |

> **Why defer AI?** It's expensive (LLM API calls) and orthogonal to monetization. The user can subscribe to ฿99/pro and use the product without AI. Building AI is its own phase with a separate design doc.

---

## 2. New Tables (Migration 0005)

| Table | Purpose |
|---|---|
| `payments` | One row per Stripe payment event (invoice.paid, etc.). Audit trail. |
| `report_schedules` | Cron-like schedule for email PDF reports. |

### 2.1 `payments`

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | |
| `tenant_id` | UUID NOT NULL FK→tenants CASCADE | |
| `subscription_id` | UUID NOT NULL FK→subscriptions CASCADE | |
| `provider` | TEXT NOT NULL | 'stripe' for now; extensible |
| `external_invoice_id` | TEXT NOT NULL | Stripe invoice id |
| `amount_cents` | BIGINT NOT NULL | |
| `currency` | CHAR(3) NOT NULL | |
| `status` | TEXT NOT NULL | `paid`, `failed`, `refunded`, `open` |
| `paid_at` | TIMESTAMPTZ | |
| `metadata` | JSONB NOT NULL DEFAULT `{}` | Stripe raw payload |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

UNIQUE(provider, external_invoice_id) — dedup against Stripe webhook retries.

### 2.2 `report_schedules`

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | |
| `tenant_id` | UUID NOT NULL FK→tenants CASCADE | |
| `trading_account_id` | UUID NOT NULL FK→trading_accounts CASCADE | |
| `frequency` | TEXT NOT NULL | `daily`, `weekly`, `monthly` |
| `hour_utc` | INT NOT NULL CHECK (0..23) | when to send |
| `weekday` | INT | 0=Sun..6=Sat (weekly only) |
| `day_of_month` | INT | 1..28 (monthly only) |
| `last_sent_at` | TIMESTAMPTZ | |
| `next_send_at` | TIMESTAMPTZ NOT NULL | indexed for the worker poll |
| `recipients` | TEXT[] NOT NULL DEFAULT `{}` | override email list (empty = tenant owner email) |
| `is_active` | BOOLEAN NOT NULL DEFAULT TRUE | |
| `metadata` | JSONB NOT NULL DEFAULT `{}` | |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

UNIQUE(tenant_id, trading_account_id, frequency).

> **subscriptions** table already exists from Phase 0. We extend it with a `stripe_subscription_id TEXT NULL` column to map local rows to Stripe.

---

## 3. Stripe Integration

### 3.1 Library

`github.com/stripe/stripe-go/v76` — official Go SDK. Pulled via `go get` in implementation step.

### 3.2 Configuration

```
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...
STRIPE_PRICE_PRO_99=price_1ABC...  # Stripe Price ID for the pro_99 plan
STRIPE_PRICE_PRO_399=price_1DEF... # Stripe Price ID for the pro_399 plan
```

If `STRIPE_SECRET_KEY` is empty → use `StubBillingService` (returns fake checkout URL, accepts no webhooks, useful for local dev).

### 3.3 HTTP contract

| Method | Path | Body | Returns | Notes |
|---|---|---|---|---|
| `GET` | `/v1/billing/plans` | — | `[{code, name, price_cents, currency, interval, stripe_price_id?}]` | List of all active plans (existing plans + Stripe price id) |
| `POST` | `/v1/billing/checkout` | `{plan_code, success_url, cancel_url}` | `{checkout_url, session_id}` | Creates Stripe Checkout session; user redirected to Stripe |
| `GET` | `/v1/billing/portal` | — | `{portal_url}` | Returns Stripe Customer Portal URL (managing subscription) |
| `GET` | `/v1/billing/subscription` | — | `{plan_code, status, current_period_start, current_period_end, cancel_at_period_end, stripe_subscription_id?}` | Current sub for tenant |
| `POST` | `/v1/billing/cancel` | `{at_period_end?}` | `{status, cancel_at?}` | Cancel subscription |
| `GET` | `/v1/billing/payments` | — | `[{id, amount_cents, currency, status, paid_at}]` | Invoice history |
| `POST` | `/v1/billing/webhook` | (Stripe POST body) | `204 No Content` | Stripe webhook receiver — verifies signature, updates local state |

> **Public endpoint** `/v1/billing/webhook` is unauth'd (no JWT, no tenant) — Stripe authenticates via signature. The route skips the `auth.AuthMiddleware` but keeps the `RequestID` + `AccessLog` middleware for traceability.

### 3.4 Webhook event handlers

| Stripe event | Local effect |
|---|---|
| `checkout.session.completed` | Find the subscription, mark it active, store `stripe_subscription_id` |
| `customer.subscription.updated` | Update `status`, `current_period_start/end`, `cancel_at_period_end` |
| `customer.subscription.deleted` | Mark subscription canceled (effective immediately) |
| `invoice.paid` | Insert `payments` row |
| `invoice.payment_failed` | Insert `payments` row with status='failed'; emit a warning email (Phase 5) |

### 3.5 State machine (subscription.status)

```
created → active ⇄ past_due → canceled
                  ↓
              incomplete → incomplete_expired
```

`active` and `trialing` grant plan features. `past_due` grants grace period (3 days), then downgrades. `canceled` immediately downgrades to free.

---

## 4. Real MetaApi Adapter

### 4.1 Library

`github.com/metaapi/metaapi-go-sdk` (Cloud SDK).

### 4.2 Configuration

```
METAAPI_TOKEN=...
METAAPI_REGION=ap-southeast  # MetaApi region; new-token default
METAAPI_ACCOUNT_ID=...        # Optional default account for env-gated dev
```

### 4.3 Implementation

`internal/integration/broker/metaapi.go` — implements `broker.Provider` with `Name() == "metaapi"`. Gated by `cfg.MetaApiEnabled()`. Register in the registry next to the mock when env vars are set.

Mapping:
- `metaapi.DealHistory` → `broker.Deal` (volume in lots, price in major units → our int64 minor units)
- `metaapi.Connect` → returns real MetaApi account id (UUID)
- `metaapi.Ping` → checks streaming health
- `metaapi.FetchDeals` → calls `GetDealsByTimeRange` (or `GetDeals` for a full pull)
- `metaapi.Disconnect` → closes the streaming connection

> The MockProvider remains the default (env-unset) so the Phase 2 flow keeps working without changes.

---

## 5. Email Report Scheduling

### 5.1 HTTP contract

| Method | Path | Body | Returns |
|---|---|---|---|
| `GET` | `/v1/reports/schedules` | — | `[{id, trading_account_id, frequency, hour_utc, weekday?, day_of_month?, next_send_at, recipients, is_active}]` |
| `POST` | `/v1/reports/schedules` | `{trading_account_id, frequency, hour_utc, weekday?, day_of_month?, recipients?}` | `{id, next_send_at, ...}` |
| `PATCH` | `/v1/reports/schedules/:id` | partial body | `{...updated schedule...}` |
| `DELETE` | `/v1/reports/schedules/:id` | — | `204` |
| `POST` | `/v1/reports/schedules/:id/run` | — | `{sync_run_id}` (manual trigger) |

### 5.2 Worker

`ScheduleWorker` — every 60s poll for `is_active = TRUE AND next_send_at <= now()`. For each due schedule:
1. Generate PDF (reuse `pdfreport.Service`)
2. Send email (via `mailer.Service`)
3. Compute next `next_send_at` per `frequency`
4. Update `last_sent_at`, `next_send_at`

### 5.3 Mailer

`internal/integration/mailer/mailer.go` — `Service` interface. `SMTPMailer` impl uses `net/smtp` (built-in). Configured by:

```
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USERNAME=...
SMTP_PASSWORD=...
SMTP_FROM="Trading Journal <noreply@trading-journal.app>"
```

If SMTP_HOST is empty → `StubMailer` (logs to stdout + writes to `/tmp/mail/*.eml` for dev inspection).

### 5.4 Email content

Plain text + HTML. Subject: `Trading Journal report — <account> — <period>`. Body: greeting + 1-line summary stats + "PDF attached". Attachment: the PDF from `pdfreport.Service`.

---

## 6. Frontend

### 6.1 `/billing` page (new)

- Current plan card (name, price, next billing date, status)
- "Manage subscription" → opens Stripe Customer Portal in new tab
- "Cancel subscription" button (with confirm)
- Plan comparison table (free / pro_99 / pro_399) → click "Upgrade" → POST /v1/billing/checkout → window.location = checkout_url
- Invoice history table at the bottom

### 6.2 `/accounts` — schedules section (new)

- Per-account "Schedule report" button → opens dialog
  - Frequency dropdown (daily / weekly / monthly)
  - Hour UTC slider
  - Weekday / day-of-month conditional fields
  - Email recipients (chips, empty = tenant owner)
- Existing schedules listed below with edit + delete

### 6.3 Dashboard

- New "Upcoming report" widget on dashboard if any schedule next_send_at within 24h

---

## 7. Definition of Done — Phase 4

- [ ] Migration 0005 applied; `payments` + `report_schedules` + `subscriptions.stripe_subscription_id` exist
- [ ] Stripe integration: `StubBillingService` + `StripeBillingService`
- [ ] `POST /v1/billing/checkout` returns valid Checkout URL (or stub URL locally)
- [ ] `POST /v1/billing/webhook` verifies signature, updates `subscriptions.status` + `payments`
- [ ] `GET /v1/billing/portal` returns Customer Portal URL
- [ ] `GET /v1/billing/subscription` returns current sub for tenant
- [ ] `POST /v1/billing/cancel` cancels sub
- [ ] Real MetaApi adapter: `internal/integration/broker/metaapi.go` implements Provider, env-gated
- [ ] Email scheduling: `mailer.Service` + `ScheduleWorker` (60s poll)
- [ ] 5 schedule endpoints
- [ ] FE `/billing` page with plan comparison + checkout flow
- [ ] FE `/accounts` schedules section
- [ ] Unit tests: billing service (stub), scheduler (deterministic clock), mailer interface
- [ ] `go build ./...` + `go test -race -short ./...` + `pnpm build` all green
- [ ] AGENTS.md updated: Phase 4 done, "Adding a new billing provider" + "Adding a new mailer backend" guides

---

## 8. Implementation Order (Phase 4)

1. **Design doc** (this file)
2. **Migration 0005** + domain + store
3. **Billing interface** + `StubBillingService` + unit test
4. **StripeBillingService** + `POST /v1/billing/checkout` + portal endpoint
5. **Webhook handler** with signature verification
6. **Real MetaApi adapter** + env-gated registry
7. **Mailer** + `StubMailer` + SMTP impl
8. **ScheduleWorker** + 5 endpoints
9. **FE** `/billing` page + schedules section
10. **Tests** (billing + scheduler + mailer)
11. **AGENTS.md + .env.example** updates

---

## 9. Out of Scope (Phase 4)

- AI analysis (LLM trade commentary) — Phase 5
- Public API + scoped API keys — Phase 5
- Stripe Tax (computed tax rates) — Phase 5+
- Multi-currency display — Phase 5+
- Email open/click tracking (SendGrid/Mailgun webhook) — Phase 5+
- Auto-scaling the API to handle webhook bursts — Phase 5+