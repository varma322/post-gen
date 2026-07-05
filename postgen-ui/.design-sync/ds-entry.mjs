// Synthetic entry for design-sync: postgen-ui has no library build (it's a
// Vite app, not a published package), so there is no dist entry with named
// exports to bundle. This hand-written shim re-exports App under a name
// (`export * from` drops default exports, so this can't be auto-synthesized).
export { default as App } from '../src/App.jsx';
