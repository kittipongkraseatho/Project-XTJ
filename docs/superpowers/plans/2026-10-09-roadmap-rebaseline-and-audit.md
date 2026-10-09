# Trading Journal AI Roadmap Rebaseline and Audit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebaseline the Trading Journal roadmap against the attached Autonomous AI Trading target and produce an evidence-backed audit of all delivered work without implementing new trading features.

**Architecture:** Keep legacy Phase 0–10 delivery as historical/platform capability, then overlay the new R0–R5 and Phase 0–16 product roadmap. Use a canonical Markdown roadmap plus a separate audit report, update project entry-point docs, and generate a non-destructive Word handoff from the accepted content.

**Tech Stack:** Markdown, PowerShell/rg for repository evidence, Go/Vue static inventory, existing project verification commands, and the DOCX workflow from `anthropic-skills:docx`.

**Spec:** `docs/superpowers/specs/2026-10-09-trading-journal-ai-roadmap-rebaseline-design.md`

## Global Constraints

- Do not overwrite `C:\Users\kittiphong.k\Downloads\Trading_Journal_AI_Trading_Roadmap_TH.docx`; create a versioned copy in the repository.
- Do not implement Strategy, Backtest, Paper Trading, Signal, Risk, Execution, or Live Trading endpoints in this plan.
- Treat the attached document as target product/architecture guidance, not proof that any future phase is shipped.
- Preserve the repository layering, tenant isolation, audit logging, money/time/ID rules, and English-only code identifiers.
- Every status claim must have a repository evidence pointer or an explicit verification limitation.
- Use Thai for the roadmap handoff content where it mirrors the source document; use English for code-facing identifiers and commit messages.

## Review Focus

- **False completion:** legacy Phase 0–10 features must not be counted as new R2–R5 release completion; test via the release/phase matrix.
- **Safety boundary drift:** no direct AI-to-MetaApi execution path may be implied; test via the architecture/safety-gates section and route inventory.
- **Verification gaps:** claims must distinguish implemented from not verified when Go/Docker/pgvector cannot run locally; test via command evidence and a limitations section.
- **Documentation drift:** README, AGENTS.md, CI notes, and the canonical roadmap must agree; test with targeted `rg` checks for stale Phase 0 claims and roadmap links.
- **Non-destructive artifact handling:** the source DOCX must remain byte-for-byte untouched; test by comparing source hash before/after and validating the generated copy.

### Task 1: Freeze source context and evidence inventory

**Files:**
- Read: `C:\Users\kittiphong.k\Downloads\Trading_Journal_AI_Trading_Roadmap_TH.docx`
- Read: `AGENTS.md`, `README.md`, `apps/api/migrations/*.up.sql`, `apps/api/internal/**`, `apps/web/app/**`, `.github/workflows/*`, `infra/*`
- Output evidence notes in: `docs/roadmaps/2026-10-09-trading-journal-implementation-audit.md`

**Interfaces:**
- Consumes: attached roadmap target and current repository.
- Produces: a traceable inventory of migrations, domains, routes, workers, frontend pages, tests, CI, and known environment limitations.

- [ ] **Step 1: Extract the source roadmap and record its instruction boundary**

  Preserve the distinction between product target instructions (R0–R5, Phase 0–16, safety gates) and the user request (docs-only rebaseline plus audit before implementation).

- [ ] **Step 2: Inventory shipped evidence**

  Record the existing `0001`–`0011` migrations, legacy Phase 0–10 specs, relevant API/FE modules, workers, routes, and git commits. Use exact repository-relative links in the audit.

- [ ] **Step 3: Record verification availability**

  Capture the commands attempted and their result, including the unavailable Go toolchain, frontend dependency/test status, Docker/pgvector availability, and any stale path/configuration blockers.

### Task 2: Write the canonical Thai roadmap

**Files:**
- Create: `docs/roadmaps/2026-10-09-trading-journal-ai-trading-roadmap-th.md`

**Interfaces:**
- Consumes: attached roadmap and design spec.
- Produces: version-controlled roadmap that is the source for project docs and the Word handoff.

- [ ] **Step 1: Add product vision, pricing baseline, architecture principles, technology baseline, and R0–R5 release model**

  Keep the source meaning, but add a short “Current implementation overlay” explaining that legacy Phase 0–10 capabilities are platform history and do not imply Autonomous Trading readiness.

- [ ] **Step 2: Add Phase 0–16 definitions of done and safety gates**

  Preserve the required order: data/domain correctness → intelligence → validation → paper → signal/risk/execution → live → continuous improvement.

- [ ] **Step 3: Add current baseline and next decision gate**

  Mark R0/R1 substantially implemented, R2/R3 partial, R4/R5 not started, and name Strategy/Strategy Versioning as the next product work only after the audit is accepted.

### Task 3: Produce the implementation audit matrix

**Files:**
- Create: `docs/roadmaps/2026-10-09-trading-journal-implementation-audit.md`

**Interfaces:**
- Consumes: evidence inventory and canonical roadmap.
- Produces: audit report suitable for release planning and future task decomposition.

- [ ] **Step 1: Map legacy Phase 0–10 to new R0/R1 and cross-cutting capabilities**

  Cover auth/tenant/RBAC, imports/normalization/dedup, journal/tags/images, analytics, broker sync/MetaApi REST, billing/Stripe state machine, webhooks, AI analysis/RAG, public API write scopes, idempotency, and workers.

- [ ] **Step 2: Map every new Phase 0–16**

  For each phase include status, evidence, missing definition-of-done items, verification state, dependencies, and the next release gate. Explicitly mark Strategy/Versioning, behavior intelligence, market regime, Backtest/OOS/WFA, Paper Trading, Signal, Risk/Execution, and autonomous loop as absent where applicable.

- [ ] **Step 3: Add findings and remediation queue without implementing fixes**

  Record stale README/Makefile, CI pgvector mismatch, unverified Go commands, and `float64` money-model violations as findings with priority and recommended follow-up phase. Keep deferred items from AGENTS.md separate from current gaps.

### Task 4: Update project entry-point documentation

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`

**Interfaces:**
- Consumes: canonical roadmap and audit report.
- Produces: consistent project status and navigation for humans and coding agents.

- [ ] **Step 1: Replace stale README status and phase roadmap**

  Link to the canonical roadmap and audit; state that legacy Phase 0–10 is delivered platform history and that the new target is currently R2 preparation, not AI live trading.

- [ ] **Step 2: Add a roadmap rebaseline section to AGENTS.md**

  Preserve the detailed legacy phase notes, but add the new release baseline, current gate, target safety boundaries, and links to the audit. Do not delete historical extension guidance.

- [ ] **Step 3: Validate documentation consistency**

  Run targeted `rg` checks to ensure no entry-point doc still claims “Phase 0 in progress” or lists the obsolete four-phase roadmap as current.

### Task 5: Generate and validate the Word handoff

**Files:**
- Create: `docs/roadmaps/2026-10-09-trading-journal-ai-trading-roadmap-th.docx`
- Preserve: `C:\Users\kittiphong.k\Downloads\Trading_Journal_AI_Trading_Roadmap_TH.docx`

**Interfaces:**
- Consumes: canonical Markdown roadmap and audit findings.
- Produces: a readable, versioned Word artifact with revision date and current implementation overlay.

- [ ] **Step 1: Create the revised DOCX without touching the source**

  Use the DOCX skill workflow and include headings, release/phase tables, safety gates, current status, and audit summary. Keep the original as the source reference.

- [ ] **Step 2: Validate the generated artifact**

  Validate the ZIP/XML structure, extract text for content parity, render to PDF/images if the office tooling is available, and compare the original source hash before/after.

### Task 6: Final review and handoff

**Files:**
- Read: all files created/modified above

- [ ] **Step 1: Review scope and claims**

  Check that no future phase is labeled shipped without evidence, no safety gate is weakened, and no deferred item was pulled into scope.

- [ ] **Step 2: Run proportionate verification**

  Run Markdown/link checks and available `make lint`, `make test`, and `make build` equivalents; report unavailable tools and failures verbatim rather than claiming success.

- [ ] **Step 3: Report changed files, database/API/frontend impact, environment changes, tests, known issues, and quickstart**

  State explicitly that this rebaseline changes documentation only and makes no database/API/frontend/runtime change.

