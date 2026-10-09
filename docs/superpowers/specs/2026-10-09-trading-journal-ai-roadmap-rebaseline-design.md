# Trading Journal AI Trading Roadmap Rebaseline Design

## Context

The attached `Trading_Journal_AI_Trading_Roadmap_TH.docx` is a product target document. It defines the desired evolution from Trading Journal to Autonomous AI Trading, including architecture boundaries and safety gates. It is not an instruction to claim future phases are shipped or to start live trading immediately.

The user request is narrower: rebaseline the project roadmap against that target and audit everything already delivered. The first deliverable is a reviewable plan; implementation of the documentation changes starts only after plan approval.

## Approaches considered

1. **Docs-only rebaseline with evidence audit (recommended).** Preserve the original attachment, create a version-controlled canonical roadmap and audit report, and update stale project-facing docs. This gives the team one source of truth while keeping legacy Phase 0–10 delivery visible as platform history.
2. **Edit the downloaded Word file in place.** Fastest for a single handoff, but it would overwrite the source, lose review history, and leave the repository without a versioned roadmap.
3. **Implement the first new product phase immediately.** This would create Strategy/Versioning work before the roadmap baseline and audit are accepted. It expands scope and makes it harder to distinguish shipped work from planned work.

The recommended approach is #1, with a revised `.docx` generated as a handoff artifact after the Markdown source and audit are accepted.

## Target design

### Canonical documents

- `docs/roadmaps/2026-10-09-trading-journal-ai-trading-roadmap-th.md` — normalized Thai roadmap based on the attachment, with the R0–R5 release model and Phase 0–16 details.
- `docs/roadmaps/2026-10-09-trading-journal-implementation-audit.md` — evidence-based mapping from the new roadmap to actual migrations, domains, handlers, frontend surfaces, workers, tests, and known blockers.
- `docs/roadmaps/2026-10-09-trading-journal-ai-trading-roadmap-th.docx` — polished Word handoff generated from the accepted roadmap/audit content; the downloaded original remains untouched.

### Status model

Each roadmap phase and release uses exactly one status:

- `Implemented` — code and an executable verification path exist.
- `Partial` — meaningful code exists, but the phase definition of done or release gate is incomplete.
- `Not started` — no production implementation matching the target boundary was found.
- `Not verified` — the implementation may exist, but the available environment could not execute its verification command.
- `Deferred` — explicitly outside the current scope; not counted as a defect.

Legacy Phase 0–10 features remain recorded as shipped platform capabilities. They must not be relabeled as completion of the new R2–R5 product releases when their required behavior/validation gates are absent.

### Baseline conclusion to encode

- Legacy delivery through Phase 10 is present in git history and code.
- New R0 Foundation is substantially implemented, subject to verification/documentation blockers.
- New R1 Journal MVP is substantially implemented: accounts, imports, journal, images, dashboard, and analytics exist.
- New R2 Trading Intelligence is incomplete: analytics exists, but behavior intelligence and Strategy/Strategy Versioning are not present.
- New R3 AI Research is incomplete: AI commentary, weekly recap, and RAG chat exist as platform capabilities, but Market Intelligence, candidate strategies, Backtest, OOS, Walk Forward, and Paper Trading are absent.
- New R4 AI Trading Beta and R5 Autonomous Trading are not started.
- The audit must call out the existing verification/documentation blockers: stale README/Makefile references, CI/database image mismatch for pgvector integration coverage, unavailable Go toolchain in the current host, and the money model's `float64` usage against the repository invariant.

### Safety boundary

The rebaseline must preserve the attachment's hard boundaries: AI cannot call MetaApi directly; all orders pass through Risk Engine and Execution Engine; new strategies require Backtest → OOS → Walk Forward → Paper Trading before live; live changes require versioning, permissions, audit records, and emergency stop. No implementation task in this rebaseline creates an order-execution endpoint.

## Acceptance criteria

1. The revised roadmap distinguishes the attachment's target instructions from the user's docs-only request.
2. Every new Phase 0–16 and R0–R5 item has a status, evidence pointer, and gap/next gate.
3. Legacy Phase 0–10 delivery is mapped without falsely marking new AI Trading releases complete.
4. README and AGENTS.md point to the canonical roadmap and audit, and no longer claim Phase 0 is current.
5. The revised Word document is created without overwriting the source attachment.
6. Markdown links, document structure, and generated Word output are validated; runtime code is unchanged in this scope.
