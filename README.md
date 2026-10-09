# Trading Journal

Multi-tenant SaaS for traders — manual MT5 history upload, auto-sync via MetaApi, analytics, journal, risk management.

> **Status:** Legacy Phase 0–10 platform delivery is implemented. The product roadmap is now rebaselined toward R0 Foundation → R5 Autonomous Trading; R2 Trading Intelligence is the next incomplete product gate. See the [canonical roadmap](./docs/roadmaps/2026-10-09-trading-journal-ai-trading-roadmap-th.md) and [implementation audit](./docs/roadmaps/2026-10-09-trading-journal-implementation-audit.md).

---

## Stack

- **Monorepo:** `apps/api` (Go) · `apps/web` (Nuxt 3) · `packages/shared` (TS types) · `infra` (docker-compose + env)
- **Backend:** Go 1.23+ · Echo v4 · slog (JSON) · sqlc · golang-migrate · argon2id · EdDSA JWT
- **Frontend:** Nuxt 3 · Vue 3 · Vuetify 3 · Pinia · TypeScript · `@vite-pwa/nuxt`
- **DB:** PostgreSQL 16 · **Cache:** Redis 7
- **OAuth:** Google · Line (Phase 0); GitHub (Phase 1+)
- **CI:** GitHub Actions

---

## Quickstart (local dev)

```bash
# 1. Clone & enter
cd "D:\bot\Trading Journal"

# 2. Install tools
make deps          # go install sqlc, etc.
go install github.com/air-verse/air@latest

# 3. Start Postgres + Redis
cd infra && docker compose up -d && cd ..

# 4. Apply migrations
make migrate-up

# 5. Run API + Web (hot reload)
make dev-api      # terminal 1 — http://localhost:8080
make dev-web      # terminal 2 — http://localhost:3000
```

Open http://localhost:3000, click **Sign up**, register, and you land on `/dashboard`.

For OAuth (Google / Line), copy `infra/.env.example` → `infra/.env` and fill the `OAUTH_*_CLIENT_ID` / `OAUTH_*_CLIENT_SECRET` for the providers you want to enable. Empty env vars = provider hidden on login page.

---

## Layout

```
apps/
├── api/                # Go + Echo backend
│   ├── cmd/api/        # entrypoint
│   ├── internal/       # config, server, auth, tenant, rbac, observability, httpapi, domain, service, store
│   ├── migrations/     # golang-migrate
│   ├── queries/        # sqlc
│   └── Dockerfile
└── web/                # Nuxt 3 frontend
    ├── app/            # pages, components, composables, stores, middleware
    ├── public/icons/   # PWA icons
    └── Dockerfile

packages/
└── shared/             # shared TS types (FE ↔ BE contract)

infra/
├── docker-compose.yml
└── .env.example

docs/superpowers/specs/ # phase-0 design doc
.github/workflows/       # CI
```

---

## Common commands

| Command | What it does |
|---|---|
| `make help` | List all targets |
| `make deps` | Install Go tools (sqlc, air, golangci-lint) |
| `make up` / `make down` | docker-compose up / down |
| `make migrate-up` / `make migrate-down` | Apply / rollback migrations |
| `make sqlc` | Regenerate sqlc code |
| `make dev-api` / `make dev-web` | Run with hot reload |
| `make test` | All tests (Go + FE) |
| `make lint` | Lint everything |
| `make build` | Production builds |

---

## Documentation

- **[Phase 0 design](./docs/superpowers/specs/2026-10-03-trading-journal-phase0-design.md)** — full architecture, data model, security, observability, testing, DoD
- **[AGENTS.md](./AGENTS.md)** — agent-facing project context (where to find things, how to extend)
- **[AI Trading roadmap](./docs/roadmaps/2026-10-09-trading-journal-ai-trading-roadmap-th.md)** — target R0–R5 releases and Phase 0–16 gates
- **[Implementation audit](./docs/roadmaps/2026-10-09-trading-journal-implementation-audit.md)** — evidence mapping and known blockers

---

## Product roadmap

| Release | Scope | Current status |
|---|---|---|
| **R0 Foundation** | Auth · Tenant · DB/API · Worker · Security · CI/CD | Partial; release verification blocked |
| **R1 Journal MVP** | Accounts · MT5 Import · Journal · Cloudinary · Dashboard | Partial; release gates not signed off |
| **R2 Trading Intelligence** | Analytics · Behavior · Profile · Strategy/Versioning | Partial; next product gate |
| **R3 AI Research** | AI analysis · Regime · Backtest · OOS · Walk Forward | Partial; research gates incomplete |
| **R4 AI Trading Beta** | Paper · Signal · Semi-Auto · Risk Engine | Not started |
| **R5 Autonomous Trading** | Full Auto · Execution · AI Journal · Continuous Improvement | Not started |

The detailed Phase 0–16 roadmap, status overlay, safety gates, and evidence matrix live in the [roadmap](./docs/roadmaps/2026-10-09-trading-journal-ai-trading-roadmap-th.md) and [audit](./docs/roadmaps/2026-10-09-trading-journal-implementation-audit.md). Each future slice follows *Build + Test + Verify* before moving on. Live AI trading is a gated destination, not the current implementation state.

---

## License

Proprietary — internal SaaS. All rights reserved.
