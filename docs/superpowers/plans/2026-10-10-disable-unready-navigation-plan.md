# Disable Unready Navigation Items Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep Recaps, Chat, API Keys, and Webhooks visible in the authenticated navigation while making them non-interactive with a `Coming soon` tooltip.

**Architecture:** Keep the navigation definition in `apps/web/app/layouts/default.vue`, adding an explicit `enabled` flag per item. Render enabled items as the current routed buttons; render disabled items as disabled Vuetify buttons without a route binding, wrapped by a non-disabled tooltip activator so hover/focus feedback still works.

**Tech Stack:** Nuxt 3, Vue 3 `<script setup>`, TypeScript, Vuetify 3, pnpm, ESLint, Nuxt typecheck, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-10-disable-unready-navigation-design.md`

## Global Constraints

- Keep code, identifiers, and UI route contracts in English.
- Do not change pages, route guards, API routes, backend behavior, or feature availability.
- Preserve the existing navigation order and enabled-item behavior.
- Disabled items must not receive a `to` binding.
- Preserve all pre-existing user changes in `apps/web/app/composables/useApi.ts`, `infra/.env.example`, `apps/web/app/api-url.ts`, and `apps/web/tests/api-url.test.ts`.

---

### Task 1: Add disabled navigation state and tooltip rendering

**Files:**
- Modify: `apps/web/app/layouts/default.vue:9-45`
- Test: manual browser verification plus existing web checks; no new test file is needed because the behavior is a Vuetify template interaction in the app shell.

**Interfaces:**
- Consumes: existing `auth.isAuthed`, `route`, and `navItems` computed navigation list.
- Produces: navigation items with `{ title: string; to: string; enabled: boolean }`; enabled items remain routed buttons, disabled items render as disabled buttons with a `Coming soon` tooltip.

- [ ] **Step 1: Record the current navigation behavior**

Confirm the current `navItems` list and authenticated template in `apps/web/app/layouts/default.vue`. The five enabled entries must remain `Dashboard`, `Trades`, `Imports`, `Analytics`, and `Accounts`; the four disabled entries must remain `Recaps`, `Chat`, `API Keys`, and `Webhooks`.

- [ ] **Step 2: Add explicit enabled flags**

Change the computed list to include `enabled: true` for the five active entries and `enabled: false` for the four unavailable entries:

```ts
return [
  { title: 'Dashboard', to: '/dashboard', enabled: true },
  { title: 'Trades', to: '/trades', enabled: true },
  { title: 'Imports', to: '/imports', enabled: true },
  { title: 'Analytics', to: '/analytics', enabled: true },
  { title: 'Accounts', to: '/accounts', enabled: true },
  { title: 'Recaps', to: '/recaps', enabled: false },
  { title: 'Chat', to: '/chat', enabled: false },
  { title: 'API Keys', to: '/api-keys', enabled: false },
  { title: 'Webhooks', to: '/webhooks', enabled: false },
]
```

- [ ] **Step 3: Render enabled and disabled items safely**

Replace the single unconditional `v-btn` loop with an enabled branch and a disabled branch. The disabled branch must omit `:to` and use a non-disabled wrapper for the tooltip activator:

```vue
<template v-for="item in navItems" :key="item.to">
  <v-btn
    v-if="item.enabled"
    :to="item.to"
    variant="text"
    size="small"
  >
    {{ item.title }}
  </v-btn>
  <v-tooltip v-else location="bottom">
    <template #activator="{ props }">
      <span v-bind="props" class="d-inline-flex">
        <v-btn variant="text" size="small" disabled>
          {{ item.title }}
        </v-btn>
      </span>
    </template>
    <span>Coming soon</span>
  </v-tooltip>
</template>
```

- [ ] **Step 4: Run focused checks**

Run from `apps/web`:

```powershell
pnpm lint
pnpm typecheck
pnpm test:unit
```

Expected: all commands exit successfully; no generated lockfile or unrelated source changes are introduced.

- [ ] **Step 5: Verify the interaction manually**

Start the web app with `pnpm dev`, sign in or use an authenticated session, and confirm:

- Dashboard, Trades, Imports, Analytics, and Accounts navigate normally.
- Recaps, Chat, API Keys, and Webhooks remain visible and muted.
- Hovering or focusing each disabled item shows `Coming soon`.
- Clicking each disabled item does not change the current route.

- [ ] **Step 6: Review the final diff**

Run:

```powershell
git diff -- apps/web/app/layouts/default.vue
git status --short
```

Expected: only the layout change is part of this implementation; pre-existing user changes remain untouched.

