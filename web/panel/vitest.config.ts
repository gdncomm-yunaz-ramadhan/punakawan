import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

export default defineConfig({
  plugins: [svelte({ hot: false })],
  resolve: {
    conditions: ["browser"],
  },
  test: {
    environment: "jsdom",
    globals: false,
    // Auto-unmounts each rendered component after every test, per
    // @testing-library/svelte's docs - without this, successive tests in
    // the same file accumulate DOM from prior renders.
    setupFiles: ["@testing-library/svelte/vitest"],
    // Node >=22's built-in `localStorage` global (--experimental-webstorage,
    // on by default in newer Node) shadows jsdom's window.localStorage
    // polyfill inside worker threads, leaving `localStorage.setItem` etc.
    // undefined. Disabling it lets jsdom's own implementation win, which
    // is what components using window.localStorage (theme/accent
    // persistence) expect in a real browser.
    env: {
      NODE_OPTIONS: "--no-experimental-webstorage",
    },
  },
});
