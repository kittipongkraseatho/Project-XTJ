# Disabled Navigation Visual State Design

## Goal

Make unavailable navigation items visually distinct from active items so users can immediately recognize that they cannot be opened.

## Scope

Update the disabled navigation item wrapper in `apps/web/app/layouts/default.vue` only.

- Keep the existing disabled behavior and `Coming soon` tooltip.
- Apply a muted text treatment and reduced opacity to disabled items.
- Use `cursor: not-allowed` when pointing at disabled items.
- Do not change enabled navigation items, routes, or backend behavior.

## Design

Add a focused `nav-item-disabled` class to the tooltip activator wrapper. The class uses the existing dark-theme muted text token (`text-medium-emphasis`) and a modest opacity reduction, then overrides the pointer cursor to `not-allowed`. The disabled `v-btn` remains responsible for blocking activation, while the wrapper carries the visual and cursor state so the tooltip continues to work.

## Verification

- Run web lint, typecheck, and unit tests.
- Run the production web build.
- Confirm active items keep their normal appearance and disabled items are visibly muted with a not-allowed cursor.

## Acceptance criteria

1. Recaps, Chat, API Keys, and Webhooks look visibly disabled in the header.
2. Hovering those items shows a `not-allowed` cursor and the existing `Coming soon` tooltip.
3. Dashboard, Trades, Imports, and Analytics keep their current appearance and navigation behavior.
