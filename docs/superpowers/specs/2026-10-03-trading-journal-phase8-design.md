# Trading Journal — Phase 8 Design

| | |
|---|---|
| **Status** | ✅ Approved 2026-10-04 — implementation starting |
| **Date** | 2026-10-04 |
| **Phase** | 8 — Real MetaApi SDK Handshake |
| **Scope** | Replace Phase 4's stub `MetaApiAdapter` with real REST calls to MetaApi Cloud · `Connect` (provision + poll DEPLOYED) · `FetchDeals` (history API) · `Ping` (health) |
| **Out of scope** | Streaming (subscriptions API) · Account-management UI · MT5 bridge install · Risk-management endpoints |

---

## 1. Decisions Resolved

| Fork | Decision |
|---|---|
| API style | **REST API via stdlib `net/http`** (no SDK) — same approach as Phase 6 OpenAI/Anthropic providers |
| Auth | Bearer token via `auth-token` header |
| Provisioning | Find-or-create pattern: list existing accounts first, only POST if not found |
| DEPLOYED polling | Synchronous with timeout (10s default, configurable); Phase 9+ can do async polling |
| SDK | **No MetaQuotes SDK** — keeps binary lean and consistent with Phase 6 providers |
| Failure modes | 401 → `ErrProviderNotConfigured`; 5xx → retry up to 3x; 4xx other → fail-fast |

---

## 2. MetaApi REST endpoints (used)

| Method | Path | Used by |
|---|---|---|
| `GET` | `/users/current/accounts` | `Connect` — list existing accounts |
| `POST` | `/users/current/accounts` | `Connect` — create if not found |
| `GET` | `/users/current/accounts/{accountId}` | `Connect` poll + `Ping` |
| `GET` | `/users/current/accounts/{accountId}/history/deals/tickets?startTime=...&endTime=...&limit=...` | `FetchDeals` — list recent tickets |
| `GET` | `/users/current/accounts/{accountId}/history/deals/{ticket}` | `FetchDeals` — single deal |

All calls go to `https://mt-client-api-v1.{region}.agiliumtrade.ai` with `auth-token: {METAAPI_TOKEN}`.

---

## 3. Connect flow

```
1. GET /users/current/accounts → list
2. Find matching account by (login, server) pair
3. If found:
   - GET /users/current/accounts/{id} → poll status
   - When state == "DEPLOYED" → return {accountId, state: DEPLOYED}
   - Else retry until DeployTimeout (default 10s)
4. If not found:
   - POST /users/current/accounts {login, server, type, ...} → create
   - Same poll loop until DEPLOYED
5. Return ConnectResult{ExternalAccountID, State, Region}
```

Errors:
- 401 → `ErrProviderNotConfigured` (bad token)
- 404 on list → empty (proceed to POST)
- 5xx → retry with backoff
- Poll timeout → `ErrDeployTimeout`

---

## 4. FetchDeals flow

```
1. GET /users/current/accounts/{id}/history/deals/tickets?startTime=ISO&endTime=ISO&limit=1000
2. For each ticket:
   - GET /users/current/accounts/{id}/history/deals/{ticket}
   - Map → canonical Deal (Phase 4 mapping, with Volume / OpenPrice / etc. as ints)
3. Return []Deal sorted by CloseTime
```

`limit=1000` is a safe default; can be tuned. Phase 8 ships single-page fetch; pagination deferred.

---

## 5. Ping flow

```
GET /users/current/accounts/{accountId}
- 200 with state=DEPLOYED → nil
- 200 with state != DEPLOYED → ErrNotDeployed
- 401 → ErrProviderNotConfigured
- 5xx → ErrUpstream
```

---

## 6. Config (env vars)

Already wired in Phase 0 (`internal/config/config.go`):

| Env | Default | Notes |
|---|---|---|
| `METAAPI_TOKEN` | (empty) | required for real adapter |
| `METAAPI_REGION` | `ap-southeast` | |
| `METAAPI_ACCOUNT_ID` | (empty) | optional default account id |
| `METAAPI_BASE_URL` | (computed from region) | overridable for sandbox |

New:
- `METAAPI_DEPLOY_TIMEOUT_SECONDS=10`
- `METAAPI_HTTP_TIMEOUT_SECONDS=15`

---

## 7. Tests (Phase 8 unit tests, all in `metaapi_test.go`)

Use `httptest.NewServer` to mock the MetaApi REST surface:

| # | Test | What it covers |
|---|---|---|
| 1 | `TestMetaApi_Connect_FindsExistingAccount` | list → match → poll DEPLOYED |
| 2 | `TestMetaApi_Connect_CreatesWhenMissing` | list empty → POST → poll DEPLOYED |
| 3 | `TestMetaApi_Connect_PollsUntilDeployed` | 3 polls with state transitions |
| 4 | `TestMetaApi_FetchDeals_MapsToCanonical` | list tickets → fetch each → mapping |
| 5 | `TestMetaApi_Ping_OK` | DEPLOYED → nil |
| 6 | `TestMetaApi_Ping_NotDeployed` | CREATED → ErrNotDeployed |
| 7 | `TestMetaApi_Connect_Unauthorized` | 401 → ErrProviderNotConfigured |
| 8 | `TestMetaApi_FetchDeals_Empty` | no tickets → empty slice, no error |

All tests use the existing `NewMetaApiAdapter` + a `BaseURL` override pointing at the httptest server.

---

## 8. Definition of Done — Phase 8

- [ ] `MetaApiAdapter.Connect` calls real REST API (list → create if needed → poll DEPLOYED)
- [ ] `MetaApiAdapter.FetchDeals` calls history API + maps to canonical Deal
- [ ] `MetaApiAdapter.Ping` returns nil / `ErrNotDeployed` / `ErrProviderNotConfigured` correctly
- [ ] ≥8 unit tests with httptest mock server, all green
- [ ] `go build ./...` + `go test -race -short ./...` all green
- [ ] AGENTS.md: Phase 8 complete; "Configuring MetaApi" guide
- [ ] .env.example: `METAAPI_DEPLOY_TIMEOUT_SECONDS`, `METAAPI_HTTP_TIMEOUT_SECONDS`

---

## 9. Implementation Order

1. **Design doc** (this file)
2. **`MetaApiAdapter.Connect`** — list + create + poll
3. **`MetaApiAdapter.FetchDeals`** — list tickets + fetch each + map
4. **`MetaApiAdapter.Ping` + `Disconnect`** (already a no-op)
5. **httptest-based unit tests** (8 tests)
6. **AGENTS.md + .env.example** + final test run

---

## 10. Out of Scope (Phase 8)

- Streaming (Phase 9+)
- Pagination (Phase 9+)
- Real-time price feed
- Account-management UI
- MT5 bridge install (one-time setup, documented elsewhere)
- Risk-management endpoints