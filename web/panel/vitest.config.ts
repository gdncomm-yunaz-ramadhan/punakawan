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
  },
});
