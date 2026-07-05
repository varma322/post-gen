# design-sync notes for postgen-ui

## Repo shape

`postgen-ui` is **not a component library** — it's a single-page Vite/React
admin app with exactly one component, `src/App.jsx` (~1600 lines, no props,
no sub-components broken out). There is no Storybook, no `.d.ts` exports, and
`package.json` has no `main`/`module`/`exports` (it's `"private": true`).

The user explicitly chose to sync it anyway as a single degenerate
"component" named `App`, with the **floor card only** (no authored preview) —
see "Preview scope" decision below.

## Why a hand-written entry shim exists

`.design-sync/ds-entry.mjs` re-exports `App` under a name:
```js
export { default as App } from '../src/App.jsx';
```
This is required because `App.jsx` only has a default export, and the
converter's own src-synthesis fallback (`export * from "…"`) silently drops
default exports — that path alone would produce zero named exports and an
empty `window.PostgenUi`. `cfg.entry` points at this shim so the real bundle
picks up `App` under the name the config's `componentSrcMap` also uses.

## Why cssEntry points at a cache copy

`vite.config.js` in this package builds to `../web/` (repo root), embedded
into the Go binary via `web/embed.go`) — not `postgen-ui/dist`. The stale
`postgen-ui/dist/` from an old config is unused; ignore it.

`cssEntry` must resolve **inside** the package root (security bound in
`package-build.mjs`), but the real compiled Tailwind CSS lands outside it at
`../web/styles.css`. So before each re-sync: run `npm run build` from
`postgen-ui/`, then copy `../web/styles.css` to
`.design-sync/.cache/app.css` (gitignored cache — regenerate, don't hand-edit).

## Preview scope decision (2026-07-05)

User was told `App` depends on a live backend (`apiFetch("/jobs/active")`,
`localStorage` token reads) and would render as a mostly-empty/loading shell
even if "authored." Chose **floor card only** — ship `App` as a real
importable component with the auto-generated crash-prevention render, no
`.design-sync/previews/App.tsx` authored. This is not a failure state; it's
the deliberate choice for this repo. Authoring a mocked preview (stubbed
`fetch`/`localStorage`) remains a future option on any re-sync.

## Re-sync risks

- The single component's render depends on `useEffect` fetches that will
  always fail silently in the sandbox (wrapped in try/catch) — this was
  accepted, not a bug to chase.
- `cssEntry`'s cache copy is regenerated from `../web/styles.css`, which is
  Tailwind JIT output scoped to classes *currently used* in `App.jsx` — if
  the app is refactored and the cache copy goes stale, the floor card may
  render unstyled or with fallback fonts. Always rebuild before re-sync.
- If this app is ever refactored into real reusable components (buttons,
  cards, forms broken out of `App.jsx`), this whole setup (the shim, the
  single-component `componentSrcMap` pin) should be revisited — that's the
  point where design-sync starts being genuinely useful here.
