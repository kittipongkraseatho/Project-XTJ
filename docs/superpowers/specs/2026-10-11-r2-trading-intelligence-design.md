# R2 Trading Intelligence Design

## Context

The repository contains the legacy Phase 0–10 Trading Journal platform and is
now being advanced toward the R0–R5 roadmap. Analytics and journal data exist,
but the R2 product boundary is incomplete: there is no behavior profile,
detector pipeline, Strategy model, or immutable Strategy Version model.

The approved scope is to finish the safe product work that can be completed
before live trading. Public API key behavior and outbound Webhook behavior stay
unchanged. Internal authenticated product endpoints may be added where the new
R2 screens need them. No work creates a direct AI-to-MetaApi order path.

## Goals

1. Turn normalized trade history into deterministic, tenant/account-scoped
   behavior metrics and a readable behavior profile.
2. Add explainable behavior detectors for risk and execution patterns.
3. Add Strategy and immutable Strategy Version records with an explicit,
   machine-readable rule schema.
4. Allow a journal trade to reference the Strategy Version used for that trade.
5. Expose the new capabilities through existing layering and UI conventions,
   with regression, tenant-isolation, and validation tests.
6. Preserve the later safety gates: Backtest → OOS → Walk Forward → Paper
   Trading → Risk/User approval before any live execution work.

## Non-goals

- No changes to the existing public API key scopes, idempotency behavior, or
  public write endpoints.
- No changes to outbound Webhook event types, delivery, retry, or signing.
- No AI strategy candidate generation or autonomous decision authority.
- No market-data ingestion, Backtest, OOS, Walk Forward, Paper Trading,
  Signal Engine, Risk Engine, Execution Engine, or MetaApi order methods.
- No migration from `float64` money fields in this slice; that remains a
  release-hardening prerequisite and must be handled separately before
  financial validation claims.

## Design boundary

The flow is:

`normalized trades → behavior calculator → behavior detectors → behavior profile`

and:

`strategy → immutable strategy version → trade linkage`

Behavior calculation reads canonical normalized trades and produces deterministic
results. It does not call an LLM, mutate trades, or place orders. Strategy
Versions are append-only after creation; an edit creates a new version. A trade
link points to a specific version and is never silently retargeted.

All reads and writes require the tenant context. Behavior profiles are always
scoped to exactly one trading account in this slice. Services own validation and
audit writes; handlers remain thin and stores remain tenant-scoped.

## Domain model

### Behavior Profile

The profile is a read model derived from normalized trades. It contains:

- sample size and calculation window;
- win rate, expectancy, average win/loss, profit factor, and net result;
- average holding time and trade frequency;
- concentration by symbol, direction, session, and magic/EA where available;
- risk indicators for loss streaks, oversized volume, rapid re-entry, and
  repeated losses after a loss;
- detector findings with a stable code, severity, count, and evidence payload.

The calculator uses a minimum sample size of 10 completed trades. A profile
with fewer trades is valid, has `status = insufficient_data`, and reports the
sample count without inventing a score. Empty windows return a profile with a
zero sample count and the same status.

### Behavior findings

Detectors are deterministic functions over normalized trades. Initial detector
codes are:

- `overtrading_frequency` — more than 20 completed trades in a rolling 24-hour
  period;
- `revenge_after_loss` — a new trade opens within 30 minutes after a loss and
  its volume is at least 150% of the preceding losing trade;
- `loss_streak` — at least 3 consecutive losing trades;
- `risk_escalation` — volume increases by at least 150% after a loss streak;
- `symbol_concentration` — one symbol represents at least 70% of the sample.

Thresholds are explicit configuration values in the service, covered by tests,
and included in the profile calculation metadata. Detector evidence contains
trade IDs and timestamps only; it must not expose secrets or raw credentials.

### Strategy and Strategy Version

`Strategy` owns identity and lifecycle metadata: tenant, name, description,
status (`draft`, `active`, or `archived`), owner, and current version pointer.
`StrategyVersion` owns the immutable definition: strategy ID, monotonically
increasing version number, rule schema, created-by, and created-at.

The rule schema is JSON with `schema_version: 1` and exactly these sections:

- `setup`: `symbols`, `directions`, and optional session names;
- `entry`: a non-empty list of conditions using supported operators `eq`, `neq`,
  `gt`, `gte`, `lt`, `lte`, and `in`;
- `exit`: a non-empty list using the same condition format;
- `risk`: `stop_distance`, `target_distance`, and `max_volume` as positive
  numeric intents, not executable broker commands.

The service validates schema version, required sections, supported operators,
and bounded numeric values before persistence. Unknown fields are rejected to
keep future strategy execution auditable. Strategy Version records cannot be
updated or deleted through the product flow; a new version supersedes them.

Trade linkage is nullable. Existing imported trades remain unlinked. Linking a
trade stores the exact Strategy Version ID and an audit record; unlinking is
not part of the first slice.

## Persistence

Add tenant-scoped migrations for:

- `strategies`;
- `strategy_versions`;
- `trade_behavior_profiles`;
- `trade_behavior_findings`;
- the nullable strategy-version reference on the canonical trade table;
- indexes for tenant/account/window lookup and strategy/version lookup.

`trade_behavior_profiles` is unique on tenant, trading account, window start,
window end, and detector algorithm version. It stores the computed summary,
status, and algorithm version. `trade_behavior_findings` stores the profile
foreign key, stable detector code, severity, count, and JSON evidence.

The migration must enforce foreign keys, unique strategy version numbers per
strategy, a unique strategy name per tenant, immutable version rows at the
service boundary, and cascade behavior consistent with the existing tenant
model. Every query includes tenant scope.

## Internal API and frontend

Add JWT-authenticated internal product routes under the existing versioned API
conventions for:

- `POST /v1/accounts/:account_id/behavior-profile` to calculate or refresh a
  profile for an explicit UTC `from`/`to` window;
- `GET /v1/accounts/:account_id/behavior-profile` to read the latest matching
  profile;
- `GET /v1/strategies` and `POST /v1/strategies`;
- `GET /v1/strategies/:id/versions` and `POST /v1/strategies/:id/versions`;
- `PUT /v1/trades/:id/strategy-version` to link one trade to one version.

These are product endpoints, not public API-key endpoints. Their DTOs must use
the repository's error envelope, UUIDs, UTC timestamps, and existing RBAC
rules. The frontend adds behavior/profile and strategy/version views using the
existing stores/composables and preserves the dark theme. Public API and
Webhook pages and contracts are not changed by this design.

## Testing strategy

Use TDD for each vertical slice:

1. calculator tests for empty, minimum-sample, zero, loss-streak, timezone,
   and boundary-threshold cases;
2. detector regression fixtures with stable trade IDs and expected findings;
3. strategy rule-schema validation tests, including unknown operators and
   invalid numeric bounds;
4. repository/service tenant-isolation tests;
5. HTTP contract tests for validation, RBAC, tenant/account ownership,
   idempotent profile reads, and trade link behavior;
6. frontend unit tests for loading, empty, error, profile findings, strategy
   version creation, and immutable-version display.

The work is complete only when the new focused tests pass and existing
non-Docker checks remain green. Docker-backed E2E, API race tests, and the
existing Public API/Webhook verification blockers remain visible and are not
silently marked complete.

## Acceptance criteria

1. A tenant can calculate a deterministic behavior profile for an account and
   see detector findings with evidence.
2. A tenant can create a Strategy and append immutable Strategy Versions using
   validated rules.
3. A trade can be linked to one specific Strategy Version without changing
   existing import/dedup behavior.
4. Tenant isolation, RBAC, audit behavior, and error envelopes are covered by
   tests.
5. No public API key or outbound Webhook contract changes.
6. No order-execution path, live trading permission, or Risk Engine bypass is
   introduced.
7. Documentation records remaining release blockers and the next R3 gate.
