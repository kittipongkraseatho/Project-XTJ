# Trading Journal Signal Room UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Vuetify-first web presentation with a cohesive Nuxt UI v3 and Motion for Vue “Signal Room” interface across every existing route without changing API contracts, auth behavior, stores, composables, or domain functionality.

**Architecture:** Keep the current Nuxt pages, Pinia stores, and domain composables as the data/application layer. Add a presentation layer composed of a responsive app shell, shared Nuxt UI primitives, and typed display components; migrate each route to those components in bounded batches. Motion is centralized through named transition presets and used only at page/panel/state boundaries.

**Tech Stack:** Nuxt 3.13, Vue 3, TypeScript, Nuxt UI v3, Tailwind CSS, Motion for Vue (`motion-v/nuxt`), Pinia, existing composables, Vitest, Vue Test Utils, Playwright/browser smoke checks.

**Spec:** `docs/superpowers/specs/2026-10-09-trading-journal-signal-room-ui-design.md`

## Global Constraints

- Dark theme remains the default with Signal Room tokens: `#080D16`, `#101A29`, `#152235`, `#24354C`, `#F7FBFF`, `#8EA1BA`, `#B7F36A`, `#55E6D5`, `#A78BFA`, `#FF8066`.
- Nuxt UI v3 is the component system; Vuetify must be removed from `apps/web` after migration.
- Motion uses `motion-v/nuxt`, transform/opacity-first animation, stable skeleton geometry, and `prefers-reduced-motion` fallbacks.
- Preserve all existing route names, API endpoints, DTOs, composables, Pinia stores, auth middleware, permissions, and user-visible functionality.
- Do not change backend, database, migrations, billing, auth, AI, broker, worker, or deferred Phase 11+ behavior.
- Do not load fonts or assets from a third-party CDN; use local/package assets or resilient system fallbacks.
- Keep the local API/web host alignment on `127.0.0.1`.
- Code, identifiers, logs, and test names remain English; user-facing chat can remain English unless existing copy requires otherwise.
- Every task must end with a focused test/build check and a small Conventional Commit containing only that task's files.

---

## File map

### New files

- `apps/web/app/components/app/AppShell.vue` — responsive shell composition.
- `apps/web/app/components/app/AppSidebar.vue` — desktop rail and active navigation indicator.
- `apps/web/app/components/app/AppCommandBar.vue` — page context, account/date controls, usage, user menu.
- `apps/web/app/components/app/AppMobileNav.vue` — mobile primary navigation.
- `apps/web/app/components/ui/AppPageHeader.vue` — shared page heading/action layout.
- `apps/web/app/components/ui/MetricCard.vue` — semantic KPI card.
- `apps/web/app/components/ui/SignalBadge.vue` — semantic status/outcome badge.
- `apps/web/app/components/ui/DataTable.vue` — table frame and state slots.
- `apps/web/app/components/ui/ChartPanel.vue` — chart frame and state slots.
- `apps/web/app/components/ui/InsightCard.vue` — AI insight entry/panel.
- `apps/web/app/components/ui/EmptyState.vue` — action-oriented empty state.
- `apps/web/app/components/ui/LoadingSkeleton.vue` — stable loading geometry.
- `apps/web/app/composables/useMotionPresets.ts` — named Motion transition/variant presets.
- `apps/web/app/composables/useSignalTheme.ts` — shared semantic tone/token mapping.
- `apps/web/app/assets/css/signal-theme.css` — Nuxt UI/Tailwind theme tokens and base CSS.
- `apps/web/app/components/app/__tests__/AppSidebar.test.ts` — shell navigation behavior.
- `apps/web/app/components/ui/__tests__/MetricCard.test.ts` — KPI semantics and reduced-motion classes.
- `apps/web/app/components/ui/__tests__/DataTable.test.ts` — loading/empty/error/data states.
- `apps/web/app/pages/__tests__/auth-pages.test.ts` — auth form submission/error behavior.

### Modified files

- `apps/web/package.json`, `apps/web/pnpm-lock.yaml` — Nuxt UI/Tailwind/Motion dependencies and removal of Vuetify after migration.
- `apps/web/nuxt.config.ts` — modules, CSS, runtime theme setup, and removal of Vuetify transpile/style configuration.
- `apps/web/app/app.vue` — `UApp` provider and shell-level toast/overlay context.
- `apps/web/app/layouts/default.vue` — delegate to `AppShell` and preserve auth/logout behavior.
- `apps/web/app/assets/scss/global.scss` — reduce to compatibility/reset rules or remove after CSS migration.
- `apps/web/app/plugins/01.vuetify.ts` — delete after all Vuetify references are removed.
- `apps/web/app/pages/login.vue`, `register.vue`, `auth/oauth-complete.vue` — auth presentation migration.
- `apps/web/app/pages/dashboard.vue`, `trades/index.vue`, `trades/[id].vue`, `imports.vue`, `analytics.vue`, `accounts.vue` — core trading migration.
- `apps/web/app/pages/recaps.vue`, `chat.vue` — AI surface migration.
- `apps/web/app/pages/billing.vue`, `api-keys.vue`, `webhooks.vue`, `offline.vue` — operations/utility migration.
- `apps/web/app/components/{OAuthButtonGroup,SyncStatusCard,JournalEditor,ImageGrid,TagPicker,PwaInstallButton,PwaUpdateToast,*Chart.vue}` — replace Vuetify markup while preserving props and emitted behavior.
- `apps/web/app/middleware/auth.global.ts`, composables, and stores — only if presentation adapters need type-safe props; no API behavior changes.

---

## Task 1: Establish Nuxt UI, Tailwind, Motion, and Signal Room theme

**Files:**
- Modify: `apps/web/package.json`
- Modify: `apps/web/pnpm-lock.yaml`
- Modify: `apps/web/nuxt.config.ts`
- Create: `apps/web/app/assets/css/signal-theme.css`
- Modify: `apps/web/app/app.vue`
- Modify: `apps/web/app/assets/scss/global.scss`

**Interfaces:**
- Produces the Nuxt UI provider and CSS token contract consumed by every later task.
- Produces the `motion-v/nuxt` module used by shell and page components.

- [ ] **Step 1: Record the current web baseline**

Run:

```powershell
pnpm --dir apps/web lint
pnpm --dir apps/web typecheck
pnpm --dir apps/web test:unit
pnpm --dir apps/web build
```

Expected: record the existing status before dependency migration; do not alter unrelated failures from the dirty working tree.

- [ ] **Step 2: Add the presentation dependencies**

Run from `apps/web`:

```powershell
pnpm add @nuxt/ui@^3 tailwindcss motion-v
```

Keep `@pinia/nuxt`, `@vueuse/nuxt`, `@vite-pwa/nuxt`, validation packages, and chart/runtime dependencies intact until their consumers are migrated.

- [ ] **Step 3: Configure Nuxt modules and CSS entry points**

In `nuxt.config.ts`, add `@nuxt/ui` and `motion-v/nuxt` to `modules`, add `~/assets/css/signal-theme.css` to `css`, retain `srcDir: 'app/'`, and remove only Vuetify-specific `css` and `build.transpile` entries after confirming no code still imports Vuetify.

```ts
modules: ['@nuxt/ui', '@pinia/nuxt', '@vueuse/nuxt', '@vite-pwa/nuxt', 'motion-v/nuxt']
```

- [ ] **Step 4: Define the theme contract**

Create CSS variables and Tailwind/Nuxt UI color aliases for the Signal Room palette. Include `:focus-visible`, selection, scrollbar, body background, and reduced-motion rules. Do not add external font imports.

```css
:root {
  --signal-ink: #080D16;
  --signal-surface: #101A29;
  --signal-raised: #152235;
  --signal-line: #24354C;
  --signal-text: #F7FBFF;
  --signal-muted: #8EA1BA;
  --signal-lime: #B7F36A;
  --signal-cyan: #55E6D5;
  --signal-violet: #A78BFA;
  --signal-coral: #FF8066;
}

@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after { scroll-behavior: auto !important; animation-duration: 0.01ms !important; }
}
```

- [ ] **Step 5: Add the Nuxt UI application provider**

Wrap the app in `UApp` while retaining `NuxtLayout` and `NuxtPage`.

```vue
<template>
  <UApp>
    <NuxtLayout><NuxtPage /></NuxtLayout>
  </UApp>
</template>
```

- [ ] **Step 6: Run the theme smoke check**

Run:

```powershell
pnpm --dir apps/web typecheck
pnpm --dir apps/web build
```

Expected: Nuxt generates successfully with the new modules before any page migration.

- [ ] **Step 7: Commit the foundation**

```powershell
git add apps/web/package.json apps/web/pnpm-lock.yaml apps/web/nuxt.config.ts apps/web/app/app.vue apps/web/app/assets/css/signal-theme.css apps/web/app/assets/scss/global.scss
git commit -m "feat(web): add signal room UI foundation"
```

## Task 2: Build the responsive app shell and motion presets

**Files:**
- Create: `apps/web/app/components/app/AppShell.vue`
- Create: `apps/web/app/components/app/AppSidebar.vue`
- Create: `apps/web/app/components/app/AppCommandBar.vue`
- Create: `apps/web/app/components/app/AppMobileNav.vue`
- Create: `apps/web/app/composables/useMotionPresets.ts`
- Create: `apps/web/app/composables/useSignalTheme.ts`
- Modify: `apps/web/app/layouts/default.vue`
- Create: `apps/web/app/components/app/__tests__/AppSidebar.test.ts`

**Interfaces:**
- Consumes `useAuthStore`, `useRoute`, existing navigation paths, `PwaInstallButton`, and existing logout behavior.
- Produces shell props/events: `AppShell` renders the default slot; navigation components accept `items`, `activePath`, and `onLogout` behavior without owning auth state.

- [ ] **Step 1: Write the shell navigation test**

Test that authenticated navigation includes Dashboard, Trades, Imports, Analytics, Accounts, Recaps, Chat, API Keys, and Webhooks; unauthenticated navigation exposes Login/Sign up; the active item is marked by route path.

```ts
it('marks the current authenticated route active', () => {
  const wrapper = mount(AppSidebar, { props: { items, activePath: '/trades' } })
  expect(wrapper.get('[data-nav="/trades"]').attributes('data-active')).toBe('true')
})
```

- [ ] **Step 2: Implement the desktop rail**

Use Nuxt UI `UNavigationMenu`, `UButton`, `UAvatar`, and `UBadge`; add a `Motion` active indicator with `layoutId="sidebar-active"`. Keep navigation labels and routes unchanged.

- [ ] **Step 3: Implement command bar and mobile navigation**

Render page context from `useRoute`, preserve account/user controls, and collapse to a mobile top bar plus five-item bottom navigation. Secondary links remain in a menu. Ensure every icon-only button has an accessible label.

- [ ] **Step 4: Implement named Motion presets**

Export typed variants/transitions from `useMotionPresets()`:

```ts
export function useMotionPresets() {
  return {
    pageEnter: { initial: { opacity: 0, y: 8 }, animate: { opacity: 1, y: 0 } },
    panelReveal: { initial: { opacity: 0, scale: 0.985 }, animate: { opacity: 1, scale: 1 } },
    transition: { duration: 0.24, ease: 'easeOut' },
  }
}
```

Use a reduced-motion media check to return zero-transform variants while preserving immediate visibility.

- [ ] **Step 5: Delegate the default layout to the shell**

Replace the existing `v-app-bar`, `v-main`, and `v-footer` structure with `AppShell`, preserving `PwaUpdateToast`, `PwaInstallButton`, auth state, and logout behavior.

- [ ] **Step 6: Run focused shell tests**

Run:

```powershell
pnpm --dir apps/web test:unit -- app/components/app/__tests__/AppSidebar.test.ts
pnpm --dir apps/web typecheck
```

Expected: navigation tests and typecheck pass.

- [ ] **Step 7: Commit the shell**

```powershell
git add apps/web/app/components/app apps/web/app/composables/useMotionPresets.ts apps/web/app/composables/useSignalTheme.ts apps/web/app/layouts/default.vue
git commit -m "feat(web): add signal room app shell"
```

## Task 3: Create shared UI primitives and state patterns

**Files:**
- Create: `apps/web/app/components/ui/AppPageHeader.vue`
- Create: `apps/web/app/components/ui/MetricCard.vue`
- Create: `apps/web/app/components/ui/SignalBadge.vue`
- Create: `apps/web/app/components/ui/DataTable.vue`
- Create: `apps/web/app/components/ui/ChartPanel.vue`
- Create: `apps/web/app/components/ui/InsightCard.vue`
- Create: `apps/web/app/components/ui/EmptyState.vue`
- Create: `apps/web/app/components/ui/LoadingSkeleton.vue`
- Create: `apps/web/app/components/ui/__tests__/MetricCard.test.ts`
- Create: `apps/web/app/components/ui/__tests__/DataTable.test.ts`

**Interfaces:**
- `MetricCard`: `label: string`, `value: string | number`, `tone?: 'neutral' | 'positive' | 'negative' | 'ai' | 'warning'`, `trend?: string`, `pulse?: boolean`.
- `SignalBadge`: `label: string`, `tone: 'win' | 'loss' | 'pending' | 'connected' | 'error' | 'neutral'`.
- `DataTable`: `loading`, `error`, `empty`, `columns`, and default row slot; pagination stays outside so existing page behavior is preserved.
- `ChartPanel`: `title`, `description?`, `loading`, `empty`, `error?`, and default chart slot.

- [ ] **Step 1: Write component tests for semantic tones and states**

Cover positive/negative classes, visible labels for icon semantics, loading skeleton presence, empty copy/action slot, and error slot rendering.

- [ ] **Step 2: Implement `MetricCard` and `SignalBadge`**

Use Nuxt UI `UCard`, `UBadge`, and `UIcon`; ensure positive/negative values include text or sign context, not color alone.

- [ ] **Step 3: Implement `DataTable` and `ChartPanel`**

Use `UTable`/`UCard` primitives and stable slots. Keep dimensions stable between loading and loaded states. Add responsive overflow behavior to tables.

- [ ] **Step 4: Implement page header, insight, empty, and skeleton components**

Use one action label per state, preserve sentence case, and add `data-testid` hooks for tests and browser smoke checks.

- [ ] **Step 5: Add Motion at component boundaries**

Apply `panelReveal` to `ChartPanel` and `InsightCard`, `dataReveal` to `MetricCard` only when `pulse` is true, and no perpetual animation.

- [ ] **Step 6: Run focused primitive tests**

```powershell
pnpm --dir apps/web test:unit -- app/components/ui/__tests__
pnpm --dir apps/web lint
pnpm --dir apps/web typecheck
```

Expected: all new component tests pass without lint/type errors.

- [ ] **Step 7: Commit shared primitives**

```powershell
git add apps/web/app/components/ui
git commit -m "feat(web): add signal room UI primitives"
```

## Task 4: Migrate authentication and public entry states

**Files:**
- Modify: `apps/web/app/pages/login.vue`
- Modify: `apps/web/app/pages/register.vue`
- Modify: `apps/web/app/pages/auth/oauth-complete.vue`
- Modify: `apps/web/app/components/OAuthButtonGroup.vue`
- Create: `apps/web/app/pages/__tests__/auth-pages.test.ts`

**Interfaces:**
- Consumes existing `useAuth`, `useOAuth`, route query redirect/error values, and `OAuthButtonGroup` behavior.
- Produces the same submit payloads, redirects, error code/message rendering, and OAuth links.

- [ ] **Step 1: Write auth behavior tests**

Test login submits `{ email, password }`, disables the submit action while loading, renders the typed error code/message, and routes to `redirect` or `/dashboard` on success. Test register retains the current validation and submission contract.

- [ ] **Step 2: Implement the Signal Room auth frame**

Use `UCard`, `UForm`, `UFormField`, `UInput`, `UButton`, `UAlert`, `USeparator`, and `UContainer`. Add the restrained equity-pulse panel as decorative-but-static-safe content.

- [ ] **Step 3: Add auth Motion**

Animate the form/pulse panel as one short staggered group; do not animate input layout or error height in a way that moves the submit button unexpectedly.

- [ ] **Step 4: Run auth tests and build**

```powershell
pnpm --dir apps/web test:unit -- app/pages/__tests__/auth-pages.test.ts
pnpm --dir apps/web typecheck
pnpm --dir apps/web build
```

- [ ] **Step 5: Commit auth migration**

```powershell
git add apps/web/app/pages/login.vue apps/web/app/pages/register.vue apps/web/app/pages/auth/oauth-complete.vue apps/web/app/components/OAuthButtonGroup.vue apps/web/app/pages/__tests__/auth-pages.test.ts
git commit -m "feat(web): refresh signal room auth screens"
```

## Task 5: Migrate dashboard and core trading surfaces

**Files:**
- Modify: `apps/web/app/pages/dashboard.vue`
- Modify: `apps/web/app/pages/trades/index.vue`
- Modify: `apps/web/app/pages/trades/[id].vue`
- Modify: `apps/web/app/pages/imports.vue`
- Modify: `apps/web/app/pages/accounts.vue`
- Modify: `apps/web/app/components/SyncStatusCard.vue`
- Modify: `apps/web/app/components/JournalEditor.vue`
- Modify: `apps/web/app/components/ImageGrid.vue`
- Modify: `apps/web/app/components/TagPicker.vue`
- Modify: `apps/web/app/components/*Chart.vue`

**Interfaces:**
- Consumes existing `useAnalytics`, `useTrades`, `useImports`, `useImages`, `useJournal`, `useTags`, `useBrokerConnections`, and dashboard store contracts unchanged.
- Produces the same route links, filters, pagination values, upload flow, journal save behavior, chart props, and sync polling behavior.

- [ ] **Step 1: Build the dashboard signal hierarchy**

Replace Vuetify cards/tables/tabs with `AppPageHeader`, account switcher, `MetricCard`, `ChartPanel`, `DataTable`, `EmptyState`, `SyncStatusCard`, and `SignalBadge`. Preserve all existing refresh/watch/onMounted logic.

- [ ] **Step 2: Migrate the trades list/detail**

Use `DataTable` with responsive overflow, semantic outcome badges, stable currency formatting, and the existing pagination model. Keep `/trades/:id` links and AI analysis actions unchanged.

- [ ] **Step 3: Migrate imports/accounts/journal pieces**

Use Nuxt UI form/dropzone/button/dialog patterns, preserve upload statuses, account limits, broker connection controls, journal editor content, image grid actions, and tag selection.

- [ ] **Step 4: Add data reveal and empty-state motion**

Animate only the first loaded panel and account selection state. Keep upload progress and sync polling readable when reduced motion is enabled.

- [ ] **Step 5: Run route-level checks**

```powershell
pnpm --dir apps/web lint
pnpm --dir apps/web typecheck
pnpm --dir apps/web build
```

Expected: core pages compile without changing composable calls or route behavior.

- [ ] **Step 6: Commit core migration**

```powershell
git add apps/web/app/pages/dashboard.vue apps/web/app/pages/trades apps/web/app/pages/imports.vue apps/web/app/pages/accounts.vue apps/web/app/components
git commit -m "feat(web): migrate core trading screens to signal room"
```

## Task 6: Migrate analytics and chart presentation

**Files:**
- Modify: `apps/web/app/pages/analytics.vue`
- Modify: `apps/web/app/components/EquityCurveChart.vue`
- Modify: `apps/web/app/components/SessionBreakdownChart.vue`
- Modify: `apps/web/app/components/DayOfWeekChart.vue`
- Modify: `apps/web/app/components/HourOfDayHeatmap.vue`
- Modify: `apps/web/app/components/EABreakdownTable.vue`

**Interfaces:**
- Consumes existing chart row props and analytics composable methods without changing response shapes.
- Produces consistent `ChartPanel`/`DataTable` layouts and the equity pulse reveal.

- [ ] **Step 1: Add an analytics page loading/error test**

Verify that loading shows stable panel skeletons, API errors render actionable error copy, and the account selector triggers the existing refresh path.

- [ ] **Step 2: Migrate KPI strip and account context**

Render all existing KPI values through `MetricCard`, preserving number formatting, positive/negative tones, and account selection.

- [ ] **Step 3: Wrap advanced charts and tables**

Use `ChartPanel` for sessions, days, hours, and equity; use `DataTable` for EA/symbol/daily P/L tables. Preserve chart rendering internals unless Vuetify wrappers are present.

- [ ] **Step 4: Add reduced-motion chart fallback**

Charts render immediately under reduced motion while normal mode can reveal the chart line once after data arrives.

- [ ] **Step 5: Run analytics checks**

```powershell
pnpm --dir apps/web test:unit -- app/pages
pnpm --dir apps/web typecheck
pnpm --dir apps/web build
```

- [ ] **Step 6: Commit analytics migration**

```powershell
git add apps/web/app/pages/analytics.vue apps/web/app/components/*Chart.vue apps/web/app/components/HourOfDayHeatmap.vue apps/web/app/components/EABreakdownTable.vue
git commit -m "feat(web): refresh signal room analytics"
```

## Task 7: Migrate AI recap and chat experiences

**Files:**
- Modify: `apps/web/app/pages/recaps.vue`
- Modify: `apps/web/app/pages/chat.vue`
- Modify: `apps/web/app/components/InsightCard.vue`

**Interfaces:**
- Consumes existing `useRecap`, `useChat`, `useImports`, and `useAnalysis` contracts unchanged.
- Produces the same thread creation/deletion, polling, account scope, budget, message send, and timeout behavior.

- [ ] **Step 1: Add chat state tests**

Test that selecting a thread loads messages, sending disables the composer, pending typing renders, a successful poll refreshes messages/usage, and timeout exposes the existing error path.

- [ ] **Step 2: Migrate recap layout**

Use `AppPageHeader`, account/week context, `InsightCard`, usage indicator, and status/empty states. Keep async polling and budget semantics.

- [ ] **Step 3: Migrate chat layout**

Use a responsive thread sidebar, `UCard` message surface, `UTextarea`/`UInput` composer, `UButton`, `UBadge`, and `InsightCard` for assistant metadata. Preserve the 70vh-like usable workspace without hard-coding fragile heights on mobile.

- [ ] **Step 4: Add purposeful AI motion**

Use presence for pending typing and message arrival, a short thread selection transition, and no animated background loops. Respect reduced motion.

- [ ] **Step 5: Run AI checks**

```powershell
pnpm --dir apps/web test:unit -- app/pages/__tests__
pnpm --dir apps/web lint
pnpm --dir apps/web typecheck
```

- [ ] **Step 6: Commit AI migration**

```powershell
git add apps/web/app/pages/recaps.vue apps/web/app/pages/chat.vue apps/web/app/components/InsightCard.vue
git commit -m "feat(web): refresh signal room AI surfaces"
```

## Task 8: Migrate billing, API keys, webhooks, and utility states

**Files:**
- Modify: `apps/web/app/pages/billing.vue`
- Modify: `apps/web/app/pages/api-keys.vue`
- Modify: `apps/web/app/pages/webhooks.vue`
- Modify: `apps/web/app/pages/offline.vue`
- Modify: `apps/web/app/components/PwaInstallButton.vue`
- Modify: `apps/web/app/components/PwaUpdateToast.vue`

**Interfaces:**
- Consumes existing billing, API key, webhook, PWA, and auth composables unchanged.
- Produces the same scopes, one-time secret display, revoke/delete confirmations, delivery drill-down, and PWA update/install actions.

- [ ] **Step 1: Migrate billing and plan states**

Use `MetricCard`, `SignalBadge`, `DataTable`, `UButton`, and `UModal` while preserving checkout/manage URLs and payment status semantics.

- [ ] **Step 2: Migrate API key scope management**

Keep read/write grouping and “Select all write scopes” behavior. Use accessible checkbox groups, one-time secret reveal, and explicit destructive confirmation.

- [ ] **Step 3: Migrate webhook management**

Use consistent subscription table, create dialog, event badges, revoke action, and delivery detail states. Never display or log webhook secrets more than the existing one-shot behavior.

- [ ] **Step 4: Migrate offline/PWA feedback**

Use shared alert/toast patterns and recovery actions; preserve PWA lifecycle behavior.

- [ ] **Step 5: Run operations checks**

```powershell
pnpm --dir apps/web lint
pnpm --dir apps/web typecheck
pnpm --dir apps/web build
```

- [ ] **Step 6: Commit operations migration**

```powershell
git add apps/web/app/pages/billing.vue apps/web/app/pages/api-keys.vue apps/web/app/pages/webhooks.vue apps/web/app/pages/offline.vue apps/web/app/components/PwaInstallButton.vue apps/web/app/components/PwaUpdateToast.vue
git commit -m "feat(web): migrate signal room operations screens"
```

## Task 9: Remove Vuetify and close presentation-layer cleanup

**Files:**
- Modify: `apps/web/nuxt.config.ts`
- Modify: `apps/web/package.json`
- Modify: `apps/web/pnpm-lock.yaml`
- Delete: `apps/web/app/plugins/01.vuetify.ts`
- Modify or delete: `apps/web/assets/scss/global.scss`

- [ ] **Step 1: Prove no Vuetify references remain**

Run:

```powershell
rg -n "v-|vuetify|V[A-Z]" apps/web/app apps/web/nuxt.config.ts apps/web/package.json
```

Expected: no component/template/plugin/import references remain; review false positives in prose or variable names manually.

- [ ] **Step 2: Remove Vuetify dependency and config**

Run from `apps/web`:

```powershell
pnpm remove vuetify @mdi/font
```

Remove Vuetify CSS, plugin registration, and transpile entry; retain icons only if the migrated UI uses a supported Nuxt UI icon source already installed.

- [ ] **Step 3: Run final web quality checks**

```powershell
pnpm --dir apps/web lint
pnpm --dir apps/web typecheck
pnpm --dir apps/web test:unit
pnpm --dir apps/web build
```

Expected: all checks pass and the generated app contains no Vuetify runtime dependency.

- [ ] **Step 4: Commit cleanup**

```powershell
git add apps/web
git commit -m "refactor(web): remove vuetify after signal room migration"
```

## Task 10: Browser smoke verification and completion review

**Files:**
- Modify only if verification exposes a presentation defect in the migrated files.
- No new product behavior or API files.

- [ ] **Step 1: Start the local web/API stack using the existing `infra/.env`**

Use the existing local process workflow and open `http://127.0.0.1:3000/login`; do not print database, Redis, JWT, OAuth, or API secrets.

- [ ] **Step 2: Verify auth flows**

Check login success, invalid credentials error, register form, redirect query preservation, and OAuth error presentation.

- [ ] **Step 3: Verify core routes**

Check dashboard empty/data state, account switching, trades pagination/detail, imports progress/error, analytics charts, and account connection states.

- [ ] **Step 4: Verify AI and operations routes**

Check recap loading/empty/data, chat thread selection/send/pending/error, billing state, API key scope/reveal/revoke, webhooks create/revoke/delivery drill-down, and offline/PWA notices.

- [ ] **Step 5: Verify responsive and reduced-motion behavior**

Use desktop and mobile viewport widths, keyboard-only navigation, focus-visible states, and browser reduced-motion emulation. Confirm no shell overflow, clipped dialogs, or unreadable table rows.

- [ ] **Step 6: Run repository-level verification**

```powershell
make lint
make build
```

Run `make test` if the existing Windows race/CGO limitation is resolved; otherwise report the exact limitation and provide the passing web/API targeted checks.

- [ ] **Step 7: Review the final diff**

```powershell
git diff --check HEAD~10..HEAD
git status --short
```

Confirm only intended UI/config/test files changed, no secrets are staged, and no deferred feature scope was introduced.

