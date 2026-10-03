# Trading Journal — Phase 1 Design

| | |
|---|---|
| **Status** | ✅ Approved 2026-10-03 (4 forks resolved) — implementation starting |
| **Date** | 2026-10-03 |
| **Phase** | 1 — Free + ฿99 Upload Journal |
| **Scope** | MT5 HTML/CSV parser · 4-table trades model · Cloudinary signed upload · Journal · Tags · Analytics · Admin stub |
| **Out of scope (later phases)** | MetaApi integration (Phase 2) · Multi-account dashboard (Phase 3) · Billing automation (Phase 4) |
| **Predecessor** | Phase 0 spec §6.2 + §6.6 |

---

## 1. Decisions Resolved

| Fork | Decision |
|---|---|
| MT5 format | HTML + CSV (full MVP per §11) |
| Trades model | Full 4-table (orders / deals / positions / derived trades per §10) |
| Cloudinary flow | BE-mediated signed upload per §16 |
| Dedup | `deals.UNIQUE(provider_deal_id, account_id)` + file hash |

---

## 2. New Tables (11 — Migration 0003)

| Table | Purpose | Notes |
|---|---|---|
| `trading_accounts` | One row per MT5 account (user-owned) | `UNIQUE(tenant_id, broker_id, account_login)` |
| `imported_files` | Uploaded HTML/CSV files with content hash | `content_hash` UNIQUE per tenant — duplicate upload guard |
| `import_jobs` | Status row per import (queued → running → completed/failed) | background-worker friendly |
| `orders` | Raw MT5 orders | one row per order event |
| `deals` | Raw MT5 deals (fills) | **UNIQUE(provider_deal_id, account_id)** — primary dedup |
| `positions` | Aggregated open→close positions (1:1 with MT5's "position" concept) | derived from deals in normalizer |
| `trades` | **Derived** business-level trade (1:N from positions for partial closes) | the "trade" the user sees |
| `trade_tags` | User-defined tags | many-to-many via `trade_tags_trades` |
| `journal_entries` | User-written notes per trade | 1:1 with `trades` |
| `trade_images` | Screenshots (Cloudinary metadata only) | per spec §18 |
| `daily_metrics` | Pre-computed daily aggregates | computed by worker after import |

ER:

```
trading_accounts ──< imported_files ──< import_jobs
                       │
                       └─< deals ──→ orders
                                  ╲
                                   └── trades ──< trade_tags_trades >── trade_tags
                                          ├──< journal_entries (1:1)
                                          └──< trade_images (1:N)
                                          └──< daily_metrics (1:N per day)
```

---

## 3. MT5 Parser Architecture

```
FE upload (multipart)
   ↓
POST /v1/imports  (auth, multipart)
   1. Validate MIME + size + extension
   2. Compute sha256 → check imported_files.content_hash (UNIQUE per tenant)
   3. INSERT imported_files + import_jobs (queued)
   4. Enqueue import_job (synchronous Phase 1 — no worker yet)
   5. → start import
```

**Import pipeline** (Phase 1 runs synchronously after upload):

```
detect_format (extension + magic bytes)
   ↓
parse  ─→ HTMLMetaQuotesParser OR CSVParser
   ↓
normalize (currency, lot size, time zone → UTC)
   ↓
deduplicate  ─→ ON CONFLICT (provider_deal_id, account_id) DO NOTHING
   ↓
aggregate_to_trades  ← match entry+exit deals, compute net P/L
   ↓
persist  (single tx: deals + orders + positions + trades + daily_metrics)
   ↓
mark import_job completed + write audit_log
```

### HTML parser

MetaQuotes HTML export structure: `<table>` with `id="deals"` or first table after `<h1>Deals</h1>`. Headers: Ticket, Open Time, Type, Volume, Symbol, Price, S/L, T/P, Close Time, Price, Commission, Swap, Profit.

Phaser approach:
1. Use `golang.org/x/net/html` tokenizer
2. Locate table by caption or `id="deals"`
3. Read `<tr>` rows, split by `<td>` columns
4. Parse dates (`YYYY.MM.DD HH:MM:SS` or `YYYY-MM-DD HH:MM:SS`)
5. Map columns → `Deal` domain entity
6. Skip rows that don't match schema (log warning, continue)

### CSV parser

Expected header: `Ticket,Symbol,OpenTime,Type,OpenPrice,CloseTime,ClosePrice,Volume,Commission,Swap,Profit,Magic,Comment`

Phaser: read first line as header, map columns by name (case-insensitive), parse with same domain mapping.

### Aggregation logic (`derived_trade`)

For each closed position (entry deal + exit deal pairs by ticket):
1. `trades.entry_deal_id` = entry deal
3. `trades.exit_deal_ids[]` = exit deal(s) (supports partial close — multiple exits)
4. `trades.net_p_l = sum(exits.profit) + entry.commission + sum(exits.commission) + sum(exits.swap) + entry.swap`
6. `trades.gross_profit = sum(exits.profit where profit > 0)`
7. `trades.gross_loss = sum(exits.profit where profit < 0)`
8. `trades.volume = sum(deal.volume for entry+exits)`
9. `trades.open_time = entry.time`
10. `trades.close_time = max(exits.time)`

Position entity:
- `ticket` (MT5's position ticket)
- `open_time` / `close_time`
- `volume_total` (entry + partial additions)
- `realized_p_l` (sum of related deals)
- `state` ('open' | 'closed')

### Time zone

MT5 exports use **broker local time** (no offset in HTML/CSV). Strategy:
1. Detect broker from HTML meta `<meta name="broker">` (if present) — else assume UTC
2. Convert to UTC at parse step — spec §6.4 mandates UTC store
3. Display in Bangkok in FE (existing convention from Phase 0)

---

## 4. Cloudinary Service

### Service contract (`internal/integration/cloudinary/service.go`)

```go
type Service interface {
    UploadImage(ctx, file io.Reader, params UploadParams) (Asset, error)
    DeleteImage(ctx, publicID string) error
    GetImageURL(publicID string, opts Transform) string
    GetThumbnailURL(publicID string) string
    GenerateSignedURL(publicID string, expires time.Duration) (string, error)
}
```

`Service` wraps `github.com/cloudinary/cloudinary-go/v2`. **Only this file imports the SDK** — spec §17.

### Upload flow (BE-mediated per §16)

```
FE: pick file → preview
   ↓
POST /v1/trades/:tradeId/images (multipart) — auth required
   1. Validate MIME (jpg/png/webp), size (≤ plan entitlement)
   2. Check plan entitlement: max_images_per_trade
   3. cloudinaryService.UploadImage with folder = `trading-journal/tenants/{tid}/trades/{tradeId}`
   4. INSERT trade_images with returned public_id, format, bytes, dimensions, secure_url
   5. Return { public_id, secure_url, thumbnail_url }
```

Folder convention (spec §14):
```
trading-journal/
    tenants/{tenant_id}/
        users/{user_id}/avatar/
        trades/{trade_id}/
        journal/{journal_id}/
```

### Signed URLs (Phase 1)

Public Cloudinary asset delivery (default public_id access) for trade screenshots within tenant. Phase 1: signed delivery OFF (keeps simple, trade-images-per-accessible). Phase 4 may add signed URLs per spec §16 if needed.

---

## 5. Analytics Service

### MVP analytics (per §30 MVP)

- Net P/L (sum of `trades.net_p_l`)
- Gross Profit / Gross Loss
- Win Rate (`wins / closed_trades`)
- Average Win / Average Loss
- Profit Factor (`gross_profit / |gross_loss|`)
- Max Drawdown (computed from equity curve)
- Expectancy (`(win_rate * avg_win) + ((1 - win_rate) * avg_loss)`)
- Trade Count
- Equity Curve (per closed trade: cumulative net_p_l)
- Daily P/L (aggregated per trade_date)

### Endpoints

```
GET /v1/analytics/overview?account_id=&from=&to=
GET /v1/analytics/equity-curve?account_id=&from=&to=
GET /v1/analytics/daily-pl?account_id=&from=&to=
GET /v1/analytics/calendar?year=&month=
```

All auth-required, tenant-scoped via store layer.

---

## 6. Journal + Tags

### Endpoints

```
GET    /v1/trades/:id/journal             — read journal entry (1:1 with trade)
PUT    /v1/trades/:id/journal             — upsert entry
DELETE /v1/trades/:id/journal             — delete entry

GET    /v1/tags                          — list tenant tags
POST   /v1/tags                          — create tag { name, color }
DELETE /v1/tags/:id

PUT    /v1/trades/:id/tags               — set tag list { tag_ids: [] }
```

### Journal entry fields (per §32)

- `strategy` (free text)
- `entry_reason` (free text)
- `exit_reason` (free text)
- `emotion` (enum: calm | neutral | fomo | anxious | confident)
- `mistake` (free text)
- `notes` (long text)

---

## 7. FE Additions

### Pages

```
/imports                        — file upload + import history
/imports/:id                    — import detail (parsing progress, errors)
/trades                         — paginated trade history (filters per §24)
/trades/:id                     — trade detail w/ screenshots + journal + tags (per §23)
/analytics                      — dashboard with charts (replaces Phase 0 placeholder)
/analytics/calendar             — daily P/L calendar
/accounts                       — list trading accounts (Phase 1: 1 account max per Free/฿99)
/admin                          — admin dashboard (Phase 1: read-only metrics stub)
```

### Components

```
<UploadDropzone/>               — drag-drop file picker
<TradeCard/>                    — single trade summary
<TradeDetailPanel/>             — symbol/direction/entry/exit/SL/TP
<JournalEditor/>                — VeeValidate + Zod
<TagPicker/>                    — multi-select with create
<EquityCurveChart/>             — Chart.js line
<DailyP/LHeatmap/>              — calendar view
```

---

## 8. Migration Commands (Phase 1)
```bash
migrate -path apps/api/migrations -database "$DATABASE_URL" up
# applies 0001 + 0002 + 0003
```

## 9. Tests

### Must-pass unit specs

- `internal/parser/html_test.go` — parse MetaQuotes sample fixture, assert rows
- `internal/parser/csv_test.go` — parse CSV fixture
- `internal/parser/aggregate_test.go` — partial close → multiple trades
- `internal/parser/dedup_test.go` — re-import same file → all deals ON CONFLICT
- `internal/analytics/overview_test.go` — equity curve math
- `internal/integration/cloudinary/service_test.go` — signed URL generation

### Must-pass integration specs

- `httpapi/imports_upload_test.go` — multipart upload → import_job completed → trades present
- `httpapi/imports_dedup_test.go` — upload same file twice → second returns 200 with 0 new
- `httpapi/trades_list_test.go` — paginated + filtered
- `httpapi/journal_upsert_test.go`
- `httpapi/analytics_overview_test.go`

---

## 10. Definition of Done — Phase 1

**Functional:**
- [ ] Migration 0003 applied, 11 new tables created
- [ ] FE user can upload HTML file → see parsed trades in <30s
- [ ] FE user can upload CSV file → see parsed trades
- [ ] Re-uploading same file does NOT duplicate trades
- [ ] Trade detail page shows symbol/direction/entry/exit/SL/TP/volume/duration/net P/L
- [ ] FE user can attach screenshot (Cloudinary) to a trade
- [ ] FE user can write journal entry for a trade
- [ ] FE user can tag a trade
- [ ] Dashboard shows: Net P/L, Win Rate, Profit Factor, Max DD, Equity Curve, Daily P/L
- [ ] Calendar shows daily P/L heatmap

**Non-functional:**
- [ ] `go test -race ./internal/parser/... ./internal/analytics/...` green
- [ ] Cloudinary signed upload never exposes API secret
- [ ] Per spec §20: plan entitlement (max_images_per_trade) enforced
- [ ] Audit logs for: import_started, import_completed, trade.image_attached

**Coverage:**
- [ ] parser ≥ 90% (critical path)
- [ ] analytics ≥ 80%
- [ ] cloudinary service ≥ 70%

---

## 11. Implementation Order (Phase 1)

1. **Migration 0003** — 11 new tables + indexes
2. **Domain entities** — trading_account, import_job, order, deal, position, trade, tag, journal, image, daily_metric
3. **Store layer** — repos for all 11 new tables (queries/ + Go wrappers)
4. **MT5 parser** — HTML parser using x/net/html
5. **CSV parser** — encoding/csv + header-driven column mapping
6. **Normalizer** — time-zone + currency + lot size
7. **Aggregator** — entry/exit deal pairing → derived trades
8. **Import service** — orchestrates parser → normalizer → dedup → aggregate → persist
9. **Cloudinary service** — wraps SDK, single import point
10. **Analytics service** — overview/equity/daily queries
11. **HTTP handlers** — imports, trades, journal, tags, analytics, images
12. **BE tests** — parser fixtures + integration tests
13. **FE upload page** — dropzone + import history
14. **FE trade list + detail** — list page with filters, detail w/ journal + tags + images
15. **FE dashboard** — replace Phase 0 placeholder with real charts
16. **FE admin stub** — basic metrics view
17. **CI** — extend api-ci.yml if new test paths
18. **Final docs + verification**

---

## 12. Coverage Map vs MASTER PROMPT §39

| §39 item | Phase 1 |
|---|---|
| Register → upload MT5 → parse → normalize → store → analytics → dashboard | ✅ |
| Journal + Tags | ✅ |
| Cloudinary screenshot | ✅ |
| Admin (stub) | ✅ (read-only metrics) |
| MT5 Auto Sync (MetaApi) | ⏭️ Phase 2 |
| 3 accounts, multi-account dashboard | ⏭️ Phase 3 |
| PDF report | ⏭️ Phase 3 |
| Billing automation | ⏭️ Phase 4 |