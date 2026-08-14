/** @type {import('tailwindcss').Config} */

// Colour tokens are the PostGen V2 Operational Interface design system, taken
// from the frontmatter of postgen_operations_dashboard/DESIGN.md.
//
// Note the prose section of that document contradicts its own frontmatter
// (#09090b / #6366f1 / #10b981 against the frontmatter's #131315 / #c0c1ff /
// #4edea3). The rendered mockups follow the frontmatter, so that is what is
// used here.
//
// The names are Material-3 semantic roles, which is what the previous emerald
// theme already used - so swapping the values re-skins every existing screen
// without touching a line of JSX.
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        // Ground and raised surfaces, a tiered dark scale that builds depth
        // through tone rather than shadow.
        "background": "#131315",
        "surface": "#131315",
        "surface-dim": "#131315",
        "surface-bright": "#39393b",
        "surface-container-lowest": "#0e0e10",
        "surface-container-low": "#1c1b1d",
        "surface-container": "#201f22",
        "surface-container-high": "#2a2a2c",
        "surface-container-highest": "#353437",
        "surface-variant": "#353437",
        "surface-tint": "#c0c1ff",

        // Text
        "on-background": "#e5e1e4",
        "on-surface": "#e5e1e4",
        "on-surface-variant": "#c7c4d7",
        "inverse-surface": "#e5e1e4",
        "inverse-on-surface": "#313032",

        // Borders
        "outline": "#908fa0",
        "outline-variant": "#464554",

        // Primary: the indigo-lavender used for active nav, primary actions,
        // and focus rings.
        "primary": "#c0c1ff",
        "on-primary": "#1000a9",
        "primary-container": "#8083ff",
        "on-primary-container": "#0d0096",
        "inverse-primary": "#494bd6",
        "primary-fixed": "#e1e0ff",
        "primary-fixed-dim": "#c0c1ff",
        "on-primary-fixed": "#07006c",
        "on-primary-fixed-variant": "#2f2ebe",

        // Secondary: emerald, carrying "healthy" - successful publishes,
        // active channels, positive trends.
        "secondary": "#4edea3",
        "on-secondary": "#003824",
        "secondary-container": "#00a572",
        "on-secondary-container": "#00311f",
        "secondary-fixed": "#6ffbbe",
        "secondary-fixed-dim": "#4edea3",
        "on-secondary-fixed": "#002113",
        "on-secondary-fixed-variant": "#005236",

        // Tertiary: amber, carrying "needs attention" - cooldowns, rate
        // limits, skipped items.
        "tertiary": "#ffb95f",
        "on-tertiary": "#472a00",
        "tertiary-container": "#ca8100",
        "on-tertiary-container": "#3e2400",
        "tertiary-fixed": "#ffddb8",
        "tertiary-fixed-dim": "#ffb95f",
        "on-tertiary-fixed": "#2a1700",
        "on-tertiary-fixed-variant": "#653e00",

        // Error: failed publishes and system errors.
        "error": "#ffb4ab",
        "on-error": "#690005",
        "error-container": "#93000a",
        "on-error-container": "#ffdad6",
      },
      fontFamily: {
        // Inter for reading, Geist for technical metadata and numeric data.
        // Both are vendored in public/fonts - the Artifact-style CSP is not a
        // factor here, but the binary must work offline, so no CDN.
        "headline": ["Inter", "system-ui", "sans-serif"],
        "display": ["Inter", "system-ui", "sans-serif"],
        "body": ["Inter", "system-ui", "sans-serif"],
        "label": ["Geist", "Inter", "system-ui", "sans-serif"],
        "mono": ["Geist Mono", "ui-monospace", "Consolas", "monospace"],
      },
      fontSize: {
        "headline-lg": ["30px", { lineHeight: "36px", letterSpacing: "-0.02em", fontWeight: "600" }],
        "headline-md": ["24px", { lineHeight: "32px", letterSpacing: "-0.02em", fontWeight: "600" }],
        "headline-sm": ["18px", { lineHeight: "28px", letterSpacing: "-0.01em", fontWeight: "600" }],
        "body-lg": ["16px", { lineHeight: "24px", letterSpacing: "-0.01em" }],
        "body-md": ["14px", { lineHeight: "20px" }],
        "body-sm": ["13px", { lineHeight: "18px" }],
        "label-md": ["12px", { lineHeight: "16px", letterSpacing: "0.02em", fontWeight: "500" }],
        "label-sm": ["11px", { lineHeight: "14px", letterSpacing: "0.03em", fontWeight: "500" }],
      },
      borderRadius: {
        sm: "0.25rem",
        DEFAULT: "0.5rem",
        md: "0.75rem",
        lg: "1rem",
        xl: "1.5rem",
        full: "9999px",
      },
      spacing: {
        "sidebar": "240px",
        "sidebar-collapsed": "64px",
      },
      maxWidth: {
        "container": "1440px",
      },
    },
  },
  plugins: [],
}
