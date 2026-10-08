# Web UI

React 19 + TypeScript + Vite, no UI framework. Styles in `web/src/styles.css` (tokens on `:root`,
dark mode via `prefers-color-scheme`). Icons in `components/Icon.tsx` (inline SVG paths), store
logos in `components/PlatformBadge.tsx` (Simple Icons, CC0; Xbox and Nintendo drawn by hand
because Simple Icons removed them).

## Structure

- `App.tsx`: shell (sidebar on desktop, bottom tab bar under 860 px), hash router (`#/library`…).
- `state/AppData.tsx`: the catalog and sources shared by every page; mutations call `putGame`
  instead of refetching.
- `features/library/`: `LibraryPage` (search, sort, A–Z rail, filters, posters/list, infinite
  scroll), `FilterPanel` (popover / bottom sheet), `PosterGrid`, `GameDetail` (the game sheet:
  hero, Overview / Copies / Edit tabs), `GameSheet` (details from metadata providers).
- `features/sources`: configured sources as rows (store logo from `SourceLogo` in
  `components/PlatformBadge.tsx`, status dot, copies, a round scan button; the row opens the
  settings dialog, which holds the last scan report and Delete), then tiles for the stores not
  added yet. Only "Scan all" is amber.
- `features/sources`, `features/providers`: settings dialogs; field help is rendered by
  `components/FieldHelp.tsx` from plain text (lines `1. …` become numbered steps, backticks become
  code).
- `api/client.ts`: Connect clients, `coverUrl`, `mediaUrl`, `proxiedImage`, `errorMessage`.
- Generated API types in `web/src/gen` (never edit; `task generate`).

## Design system ("la vitrina")

- Palette: ink `#181C2C`, slate `#262C42` (dark surfaces), mist `#EDF0F5`, paper `#FFFFFF`.
  Amber `#FFB020` is reserved for primary actions and the active state. Platform colours encode
  information (Xbox green, PlayStation blue, Nintendo red, Steam navy…), nothing else is coloured.
- Type: Archivo Variable; titles use the wide axis (`font-stretch: 118–125%`) and weight 800.
- Covers carry the colour. No all-caps labels, no decorative eyebrows, no gradients for decoration.
- Mobile: 3-column poster grid, filters as a bottom sheet, game sheet full screen, 16 px inputs
  (no iOS zoom), safe-area insets on fixed bars.

## Motion (Emil Kowalski's rules)

- Only animate what the user caused and sees rarely: dialogs, sheets, popovers. Never animate the
  grid, tabs, or keyboard-triggered changes.
- `transform`/`opacity` only; enter with `@starting-style`. Modal: scale 0.97 → 1 + fade,
  ≤ 240 ms, `--ease-out` (`cubic-bezier(0.23, 1, 0.32, 1)`). Mobile sheets slide from
  `translateY(100%)` with `--ease-drawer` (`cubic-bezier(0.32, 0.72, 0, 1)`).
- Pressable elements scale to 0.97 on `:active`. Hover lifts only under
  `@media (hover: hover) and (pointer: fine)`.
- `prefers-reduced-motion` collapses transitions.

## Patterns

- Clickable cards with buttons inside: the title is the button and stretches over the card with
  `::after { inset: 0 }`; inner buttons (platform badges) sit above with `z-index`. Never nest
  buttons.
- Scan is a keyboard loop for barcode readers: the barcode field keeps the focus (read-only, never
  disabled, while a lookup runs) and Enter in it confirms the pending result. A form whose submit
  button is disabled is not submitted by Enter, so that is handled in `onKeyDown`.
- Every string through `t()`; keys in en and es (`task i18n`).
- Remembered view preferences (sort, list/posters) use `localStorage` with try/catch; anything
  that must persist goes to the backend.

## Checking a UI change

Run `task test-server` (builds first), open http://127.0.0.1:8093/?v=N in
the browser pane (bump `v` after each rebuild: the pane caches `index.html`). Check desktop, then
`resize_window` preset `mobile` (reload), then reset with preset `desktop`. Prefer `get_page_text`
and `find` for checks; screenshots for layout. Stop it with `task test-server:stop`.
