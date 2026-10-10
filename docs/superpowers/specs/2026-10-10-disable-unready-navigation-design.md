# Disable Unready Navigation Items Design

## Goal

Prevent users from opening product areas that are not ready for the current release while keeping those areas visible as roadmap signals.

## Scope

Update the authenticated navigation in `apps/web/app/layouts/default.vue` only.

- Keep enabled: Dashboard, Trades, Imports, Analytics, Accounts.
- Keep visible but disabled: Recaps, Chat, API Keys, Webhooks.
- Disabled items must not navigate or trigger route changes.
- Disabled items must expose a clear `Coming soon` label through a tooltip.
- Do not change pages, route guards, API routes, backend behavior, or feature availability.

## Design

Represent each navigation item with an `enabled` flag. Render enabled items as the existing text buttons with their `to` route. Render disabled items as text buttons with `disabled`, without a `to` binding, and wrap them in a tooltip that explains the item is coming soon. The shared item list keeps ordering and labels in one place while making the release boundary explicit.

The disabled state is visual and interaction-safe: Vuetify owns the disabled button behavior, and omitting the route binding prevents accidental navigation from the component. The tooltip remains available on hover/focus through a non-disabled wrapper element.

## Verification

- Type-check/lint the web app.
- Run the existing web tests.
- Manually verify that the five enabled items navigate and the four disabled items remain visible, muted, and do not change the current route.

## Acceptance criteria

1. Recaps, Chat, API Keys, and Webhooks are visible but cannot be clicked to navigate.
2. Each disabled item shows `Coming soon` on hover/focus.
3. Dashboard, Trades, Imports, Analytics, and Accounts continue to behave exactly as before.
4. No unrelated files or backend contracts change.
