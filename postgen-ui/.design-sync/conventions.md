## postgen-ui conventions

**This library ships exactly one component: `App`.** It's a full admin
dashboard page (sidebar nav + tabs), not a set of reusable building blocks —
there are no separate Button/Card/Input exports. Treat `<App />` as a
reference render of the whole product, never a fragment to compose into
other layouts. It takes no props: `<App />`.

### Styling idiom: Tailwind, with this DS's own semantic color scale

No CSS-in-JS, no styled-components, no design-token CSS variables to
`var()` — styling is plain Tailwind utility classes, but drawn from this
DS's own Material-3-style semantic color scale (defined in
`tailwind.config.js` `theme.extend.colors`, compiled into the shipped CSS),
not Tailwind's stock palette:

| Role | Classes |
|---|---|
| Page background | `bg-surface` / `bg-background` |
| Body text | `text-on-surface`, muted: `text-on-surface-variant` |
| Cards / raised panels | `bg-surface-container`, `bg-surface-container-low`, `bg-surface-container-high` |
| Primary action | `bg-primary` / `text-on-primary`, filled variant: `bg-primary-container` / `text-on-primary-container` |
| Secondary / tertiary accents | `bg-secondary`, `bg-secondary-container`, `bg-tertiary`, `bg-tertiary-container` (each has an `on-*` text pair) |
| Borders | `border-outline`, `border-outline-variant` |
| Errors | `bg-error`, `text-on-error`, `bg-error-container` |

Every color has a matching `on-*` text-color token for contrast — always
pair them (e.g. a `bg-primary-container` block gets `text-on-primary-container`
text), the way `App` itself does throughout.

### Fonts

Custom font-family utilities, not raw font names: `font-headline`,
`font-display`, `font-body` (all → Inter), and `font-label` (→ Public Sans).
Both families load from a remote font host at runtime (not bundled) — a
built design keeps working the same way.

### Where the truth lives

Read `styles.css` (imports `_ds_bundle.css`, which has the compiled utility
classes) before styling anything new. `tailwind.config.js`'s
`theme.extend` block is the semantic-color source of truth if you need a
color not listed above.

### Example (idiomatic, not from a component — this DS has no smaller parts)

```jsx
<div className="bg-surface-container rounded-lg p-4 border border-outline-variant">
  <h2 className="font-headline text-on-surface text-lg">Section title</h2>
  <p className="font-body text-on-surface-variant text-sm">Supporting copy.</p>
  <button className="bg-primary text-on-primary font-label rounded-md px-3 py-1.5 mt-3">
    Primary action
  </button>
</div>
```
