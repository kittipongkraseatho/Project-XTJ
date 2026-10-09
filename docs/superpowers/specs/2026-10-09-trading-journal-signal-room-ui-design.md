# Trading Journal Signal Room UI Design

**Date:** 2026-10-09  
**Status:** Design approved by user; implementation pending  
**Scope:** Nuxt web presentation layer only

## Goal

Replace the current Vuetify-first frontend presentation with a cohesive Nuxt UI v3 design system and purposeful Motion for Vue animation, creating a distinctive dark “Signal Room” trading cockpit across all existing routes without changing the API, DTOs, stores, composables, auth flow, or domain behavior.

## Product direction

Trading Journal is a review cockpit for traders. The interface should make the current trading signal legible in seconds, then support deliberate reflection through analytics, journal, and AI surfaces. The visual personality is focused and alive: dense enough for professional review, calm enough for honest post-trade analysis, and expressive only where motion communicates state.

## Visual system

### Palette

| Token | Value | Use |
|---|---|---|
| `signal-ink` | `#080D16` | App background and deep canvas |
| `signal-surface` | `#101A29` | Cards, panels, navigation surfaces |
| `signal-raised` | `#152235` | Hovered/selected surfaces and overlays |
| `signal-line` | `#24354C` | Borders, dividers, chart guides |
| `signal-text` | `#F7FBFF` | Primary text and critical numbers |
| `signal-muted` | `#8EA1BA` | Supporting copy and metadata |
| `signal-lime` | `#B7F36A` | Primary action, active state, positive emphasis |
| `signal-cyan` | `#55E6D5` | Positive/neutral market signal and equity pulse |
| `signal-violet` | `#A78BFA` | AI surfaces and generated insight |
| `signal-coral` | `#FF8066` | Loss, error, destructive or warning state |

The theme remains dark by default. Profit/loss semantics must stay explicit and must not rely on color alone; labels, icons, and signed values accompany the semantic color.

### Typography

- Display and page headings use a geometric sans stack with strong numerals and restrained tracking.
- Body copy uses a highly legible sans stack with sentence-case labels.
- Prices, percentages, timestamps, IDs, and other dense data use a monospaced utility stack.
- Font loading must not depend on a third-party CDN. Use local/package-provided assets or a resilient system fallback and preserve the existing CSP policy.

### Signature element

The “equity pulse” is a compact signal line used in the dashboard hero, chart panels, and selected analytics summaries. It is not ambient decoration: it appears when a data set has loaded and animates once to reveal the current signal. Static fallback remains readable when motion is reduced or unavailable.

## Layout model

### Desktop

```text
┌──────────────────────────────────────────────────────────────┐
│ rail: brand / nav / account context │ command bar / user     │
├─────────────────────────────────────┼────────────────────────┤
│                                     │ page header             │
│                                     │ signal / KPI content    │
│                                     │ tables / charts / AI    │
└─────────────────────────────────────┴────────────────────────┘
```

- A persistent left rail contains brand, primary navigation, account/tenant context, and utility links.
- A compact command bar carries page title context, account switching, date context when relevant, usage/billing state, and the account menu.
- Main content uses a constrained readable width with wider analytical layouts where charts/tables need it.
- The selected navigation item uses a shared animated indicator rather than a collection of unrelated hover styles.

### Mobile

- The left rail collapses into a compact top bar.
- Primary destinations move to a bottom navigation with no more than five high-frequency items.
- Secondary/admin destinations remain available from an accessible menu.
- Tables become horizontally scrollable or switch to stacked records where the information hierarchy permits.
- Chat and journal editing retain comfortable touch targets and safe-area padding.

## Component architecture

The frontend keeps the current composable/store/data boundaries. New components are presentation-focused and receive typed props or emit typed events.

### App shell components

- `AppShell`: owns the responsive shell composition and page slot.
- `AppSidebar`: desktop navigation, active state, tenant/account context.
- `AppCommandBar`: contextual title, account/date controls, usage and user menu.
- `AppMobileNav`: mobile primary navigation.
- `AppPageHeader`: eyebrow, title, supporting description, and primary action.

### Shared UI components

- `MetricCard`: label, formatted value, semantic tone, optional trend or pulse.
- `SignalBadge`: accessible state badge for win/loss/pending/connected/error.
- `DataTable`: consistent headers, density, row hover, pagination, empty/loading/error states.
- `ChartPanel`: shared panel header, legend, loading shell, empty state, and chart slot.
- `InsightCard`: AI analysis/recap entry with provider and usage context.
- `EmptyState`: action-oriented empty state with icon, explanation, and next action.
- `LoadingSkeleton`: stable dimensions matching the loaded component to prevent layout shift.

Nuxt UI components are the default implementation primitives for buttons, inputs, forms, cards, badges, menus, tabs, tables, alerts, modals, avatars, progress, and toasts. Custom components exist only where product semantics or layout composition are not represented by a library primitive.

## Route treatment

### Auth

`/login`, `/register`, and `/auth/oauth-complete` use a focused auth frame with the Signal Room wordmark, a concise value statement, accessible form states, OAuth actions, and a restrained equity-pulse visual. The form behavior, validation, error codes, and redirect behavior remain unchanged.

### Core trading

- `/dashboard`: signal hero, account selector, KPI strip, recent trades, equity pulse, account cards, and sync state.
- `/trades` and `/trades/[id]`: data-dense trade list/detail with semantic outcome badges, clear drill-in affordances, and AI analysis entry.
- `/imports`: upload flow with progress, recent imports, actionable errors, and broker sync status.
- `/analytics`: KPI overview, time/session/EA/day/hour breakdowns, charts, and tables inside consistent chart panels.
- `/accounts`: account management framed as account context and connection health rather than configuration jargon.

### AI surfaces

- `/recaps`: period/account context, recap status, usage budget, and readable insight sections.
- `/chat`: two-pane thread workspace on desktop, single-stack flow on mobile, AI provider/budget context, readable message stream, and purposeful typing/pending transitions.

### Operations

- `/billing`: plan state, payment history, and upgrade/manage actions with clear status semantics.
- `/api-keys`: scoped access management with read/write grouping, reveal-once secret state, and destructive confirmation.
- `/webhooks`: subscription and delivery status, event badges, create/revoke flows, and delivery drill-down.

### Utility

`/offline`, PWA install, and update toasts use the same shell vocabulary and state components, with clear recovery actions.

## Motion system

Motion for Vue is added through `motion-v/nuxt`. Use a small set of named transitions rather than ad hoc animation values:

- `pageEnter`: opacity + 8px vertical reveal, short stagger for content groups.
- `panelReveal`: opacity + scale from `0.985` for data panels.
- `activeRail`: shared `layoutId` indicator for navigation selection.
- `dataReveal`: one-time chart/equity pulse reveal after data becomes available.
- `presence`: modal, drawer, toast, and pending assistant transitions.
- `interactiveLift`: small transform-only hover/focus feedback for actionable cards.

Rules:

- Animate transform and opacity; avoid layout-thrashing width/height animations.
- Do not animate every element or use perpetual decorative loops.
- Loading states use stable skeleton geometry so data arrival does not jump the page.
- Provide reduced-motion behavior through Motion configuration and CSS media fallback.
- Keyboard focus, disabled, error, and loading states remain visible without depending on animation.

## Migration strategy

1. Add and configure `@nuxt/ui` v3, Tailwind integration required by that version, and `motion-v`.
2. Establish theme tokens, base typography, focus styles, and `UApp`/app-level providers.
3. Build the new shell and migrate auth pages first.
4. Migrate dashboard, trades, imports, analytics, and accounts using shared components.
5. Migrate recaps, chat, billing, API keys, webhooks, and utility states.
6. Remove Vuetify imports, plugin, style loading, and dependency after no route or component references remain.
7. Review PWA asset loading and CSP compatibility; keep local development on the existing `127.0.0.1` host alignment.

The migration must preserve existing API calls, composables, Pinia state, route names, auth middleware, permissions, and user-visible functionality. No backend or database changes are in scope.

## Verification

Required checks after implementation:

- `pnpm lint` from `apps/web`
- `pnpm typecheck` from `apps/web`
- `pnpm test:unit` from `apps/web`
- `pnpm build` from `apps/web`
- Browser smoke checks for login, dashboard, trades, analytics, chat, API keys, webhooks, and billing.

Acceptance criteria:

- Every existing web route renders in the Signal Room system.
- No Vuetify component/import remains in `apps/web`.
- All loading, empty, error, and success states use the shared visual language.
- Layout is usable from mobile width through desktop width.
- Keyboard focus and semantic labels remain available.
- Motion respects `prefers-reduced-motion`, avoids content layout shift, and has static fallbacks.
- API contracts and authentication behavior are unchanged.

## Out of scope

- API, database, migration, billing, auth, AI, broker, or worker behavior changes.
- New product features or deferred Phase 11+ capabilities.
- Rewriting chart/data-fetching logic unless required to adapt a presentation component's props.
- Adding external analytics, font CDNs, or tracking services.
