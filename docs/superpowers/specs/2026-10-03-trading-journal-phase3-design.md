# Trading Journal — Phase 3 Design

| | |
|---|---|
| **Status** | ✅ Approved 2026-10-03 (2 forks resolved) — implementation starting |
| **Date** | 2026-10-03 |
| **Phase** | 3 — Multi-account + Session/EA/Magic + PDF Reports |
| **Scope** | Multi-account dashboard · Session analysis (Asia/London/NY/Sydney) · EA breakdown (by MT5 magic) · Day-of-week + hour-of-day analytics · PDF trade reports |
| **Out of scope (later phases)** | Billing automation · AI analysis · Public API (Phase 4) |
| **Predecessor** | Phase 1 trades model + Phase 2 broker sync (no new tables) |

---

## 1. Decisions Resolved

| Fork | Decision |
|---|---|
| Phase 3 scope | All 3 parts in this single phase (multi-account dashboard + EA/session analytics + PDF reports) |
| PDF library | `signintech/gopdf` — pure Go, MIT, drawing API. We write the template directly (not HTML→PDF) for full control over the layout and zero external runtime deps. |

> **Why no new tables?** Phase 1 already stores `magic` on every deal, and `open_time`/`close_time` on every trade. Session detection runs as a query-time bucket (`EXTRACT(HOUR FROM open_time AT TIME ZONE 'UTC')`) — no precomputed columns. Keeps the migration footprint at zero.

---

## 2. Multi-account Dashboard

### 2.1 What's new on the dashboard
- **Account selector** (Vuetify tab group or dropdown) — switches the global analytics between *All accounts*, *<account 1>*, *<account 2>*, …
- **Per-account cards** below the global stats — one compact tile per trading account (net P/L, trade count, last sync, broker connection status)
- All existing `/v1/analytics/*` endpoints already accept `?account_id=` — Phase 3 just wires the FE selector to them

### 2.2 Plan limit (Phase 3)
| Plan | Trading accounts | Broker connections |
|---|---|---|
| `free` | 1 | 1 |
| `pro_99` | 5 | 3 |

**Enforcement** lives in `CreateAccount` handler — reads tenant plan from `subscriptions`, returns 403 `PLAN_LIMIT_REACHED` if exceeded. Existing single-account tenants continue to work; new ones hit the limit only on the second create.

> Note: we do NOT retroactively fail tenants who already have more than their plan allows (Phase 1 created accounts without limit; cutting them off mid-flight is hostile). Limit applies only to new creates.

---

## 3. Session / EA / Magic Analytics

### 3.1 New HTTP endpoints

| Method | Path | Returns |
|---|---|---|
| `GET` | `/v1/analytics/session` | `[{session: 'asia'|'london'|'newyork'|'sydney', trade_count, net_p_l, win_rate, ...}]` |
| `GET` | `/v1/analytics/ea` | `[{magic: number, name?, trade_count, net_p_l, win_rate, profit_factor}]` — unnamed `magic=0` shown as 'Manual trades' |
| `GET` | `/v1/analytics/day-of-week` | `[{weekday: 0..6, weekday_name, trade_count, net_p_l, win_rate}]` |
| `GET` | `/v1/analytics/hour-of-day` | `[{hour: 0..23, trade_count, net_p_l, win_rate}]` |

All accept `?account_id=` (optional, scoped to tenant). Source table: `trades` (Phase 1 — already has `open_time`, `magic`, `net_p_l`, `outcome`).

### 3.2 Session bucket definitions (UTC)

| Session | Window (UTC) |
|---|---|
| Sydney | 22:00–07:00 |
| Asia (Tokyo) | 00:00–09:00 |
| London | 07:00–16:00 |
| New York | 13:00–22:00 |

> A single trade can fall into 2 sessions (London + NY overlap 13:00–16:00). Buckets are independent; no exclusive assignment — the SQL is `(open_time AT TIME ZONE 'UTC')::time BETWEEN '07:00' AND '16:00'`. The dashboard stacks bars, not venn-diagrams.

### 3.3 EA (magic) breakdown:
- Each MT5 EA tags orders with a unique `magic` integer (or 0 for manual).
- `magic=0` → row label 'Manual trades' in the FE
- Other magics → row label `EA #${magic}` (Phase 4 may add user-defined EA names; for Phase 3 the integer is enough)

### 3.4 Store-layer aggregations

Four new methods on `TradeRepo`:
- `SessionBreakdown(ctx, tenantID, accountID *uuid.UUID) ([]SessionRow, error)`
- `EABreakdown(ctx, tenantID, accountID *uuid.UUID) ([]EARow, error)`
- `DayOfWeekBreakdown(ctx, tenantID, accountID *uuid.UUID) ([]DayOfWeekRow, error)`
- `HourOfDayBreakdown(ctx, tenantID, accountID *uuid.UUID) ([]HourOfDayRow, error)`

Each returns `[]Row` where `Row` carries: `bucket, trade_count, wins, losses, breakevens, net_p_l, gross_profit, gross_loss, profit_factor`. SQL: `SUM(CASE WHEN outcome='win' THEN 1 ELSE 0 END)` style aggregations on `trades` table.

---

## 4. PDF Reports

### 4.1 HTTP endpoint

| Method | Path | Body | Returns |
|---|---|---|---|
| `POST` | `/v1/reports/pdf` | `{trading_account_id, from (YYYY-MM-DD), to (YYYY-MM-DD), include_equity_curve?}` | `application/pdf` binary |
| `GET` | `/v1/reports/pdf/preview` | (query params same as POST) | HTML preview (used by FE to display before download) |

> PDF is generated synchronously (no worker needed) for Phase 3 — typical report is <1s for ~1000 trades. Phase 4 may move this to the worker pool if it becomes a bottleneck.

### 4.2 PDF layout (single-page A4 portrait)

```
┌────────────────────────────────────────────┐
│  Trading Journal — Trade Report            │
│  Account: <name> · Broker: <broker>         │
│  Period: <from> — <to>                     │
│  Generated: <UTC timestamp>                │
├────────────────────────────────────────────┤
│  Summary stats (4-column grid)             │
│   Net P/L · Win rate · Profit factor · N   │
├────────────────────────────────────────────┤
│  Equity curve (optional — Phase 3 default: │
│  yes) — line plot from trade net_p_l running│
│  sum                                            │
├────────────────────────────────────────────┤
│  Symbol breakdown table (top 10 by P/L)    │
├────────────────────────────────────────────┤
│  Day-of-week bar chart (7 bars, P/L)       │
├────────────────────────────────────────────┤
│  Trade list (last 50 trades) — Symbol ·    │
│  Direction · Open · Close · Net P/L ·      │
│  Outcome                                      │
└────────────────────────────────────────────┘
```

### 4.3 `signintech/gopdf` integration

- Page size: A4 (595×842 pt)
- Fonts: Helvetica (built-in)
- Charts (equity curve + day-of-week): hand-drawn lines via `pdf.SetStrokeColor()` + `pdf.Line()` — no bitmap rasterization needed
- Trade list: `pdf.Table()` from table-style API

```go
// internal/integration/pdfreport/service.go
type Service interface {
    Generate(ctx context.Context, req ReportRequest) ([]byte, error)
}

type ReportRequest struct {
    TenantID          uuid.UUID
    TradingAccountID  uuid.UUID
    From              time.Time
    To                time.Time
    IncludeEquityCurve bool
}

type StubService struct{}
// Phase 3: returns a placeholder PDF with the title + period — full impl
// below. CI test asserts the byte slice starts with %PDF- and ends with %%EOF.
```

### 4.4 Service structure

```
internal/integration/pdfreport/
  service.go       # Service interface
  signintech.go    # impl using signintech/gopdf (production)
  stub.go          # minimal valid PDF for tests + when feature is gated off
```

---

## 5. Frontend

### 5.1 Dashboard refactor

| Old (Phase 1) | New (Phase 3) |
|---|---|
| Single dashboard | `<AccountSelector>` tabs at top: `[All accounts] [Acc1] [Acc2] …` |
| 4 stat tiles | 4 stat tiles + **per-account mini-cards** below |
| Recent trades table | Same (kept) |
| EquityCurveChart | Same (kept) |
| SyncStatusCard | Same (kept) |

`AccountSelector` writes to a Pinia store `useDashboardStore` so other pages can read the selected account_id.

### 5.2 Analytics page additions

| Component | Source |
|---|---|
| `SessionBreakdownChart.vue` — 4 stacked bars (asia/london/ny/sydney) | `/v1/analytics/session` |
| `EABreakdownTable.vue` — table of magic, label, trades, P/L, win rate | `/v1/analytics/ea` |
| `DayOfWeekChart.vue` — 7 bars (Mon–Sun) | `/v1/analytics/day-of-week` |
| `HourOfDayHeatmap.vue` — 24×7 heatmap (hour × weekday, color = P/L sign + magnitude) | `/v1/analytics/hour-of-day` |

### 5.3 PDF download buttons
- `/accounts` — per-account "Download PDF report" button → POST /v1/reports/pdf with last-30-days default → opens browser save dialog
- `/trades` — toolbar button "Export PDF" → same flow, all accounts in date filter

---

## 6. Definition of Done — Phase 3

- [ ] Phase 3 design doc committed (`docs/superpowers/specs/2026-10-03-trading-journal-phase3-design.md`)
- [ ] Plan limit enforced on `POST /v1/accounts` (1 free / 5 pro_99)
- [ ] 4 new analytics endpoints with `?account_id=` filter
- [ ] Account selector + per-account cards on dashboard
- [ ] 4 new analytics components on `/analytics` page
- [ ] `POST /v1/reports/pdf` returns valid PDF (`%PDF-…%%EOF`) with summary + trades
- [ ] FE "Download PDF report" button on `/accounts` per row
- [ ] Unit tests for analytics aggregation SQL helpers (test sql output shape with embedded pg)
- [ ] Unit test for signintech Service: produces non-empty PDF, panics-free
- [ ] `go build ./...` + `go test -race -short ./...` + `pnpm build` + `pnpm lint` all green
- [ ] AGENTS.md updated: Phase 3 done, "Adding a new analytics dimension" extension guide, "Adding a new PDF report section" guide

---

## 7. Implementation Order (Phase 3)

1. **Design doc** (this file)
2. **Plan limit** — plan metadata in db seed (or constants); enforce in `CreateAccount`
3. **Analytics aggregations** — 4 new methods on `TradeRepo` + 4 new HTTP endpoints in `phase1http.Handler`
4. **PDF service** — `internal/integration/pdfreport/` with signintech impl + stub + unit test
5. **PDF endpoint** — `POST /v1/reports/pdf` in new `phase3http` package + plan limit test
7. **Dashboard refactor** — `<AccountSelector>` + Pinia store + per-account cards on dashboard
8. **Analytics components** — `SessionBreakdownChart`, `EABreakdownTable`, `DayOfWeekChart`, `HourOfDayHeatmap` + `/analytics` page integration
9. **PDF download UI** — buttons on `/accounts` + `/trades`
10. **Tests** — analytics helpers + PDF service smoke + PDF preview HTML render
11. **AGENTS.md + .env.example** updates

---

## 8. Out of Scope (Phase 3)

- Billing automation (Phase 4)
- AI analysis (Phase 4)
- Public API + rate-limited tokens (Phase 4)
- Email report scheduling (Phase 4)
- User-defined EA name aliases (Phase 3 just shows `EA #${magic}`)
- Multi-page PDFs / report templates (Phase 3 single-page A4)
- PDF caching / async generation (Phase 3 synchronous; Phase 4 may move to worker)