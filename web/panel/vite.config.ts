import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// Dev mode proxies /api/v1 to the Go server (§21: `go run ./cmd/punakawan
// panel --dev-api --port 7331` in one terminal, `pnpm dev` in this
// package in another). Release builds are embedded into the Go binary via
// go:embed and served from the same origin, so no proxy is needed there.
export default defineConfig({
  plugins: [svelte()],
  server: {
    proxy: {
      "/api/v1": "http://127.0.0.1:7331",
    },
  },
  // Builds directly into the Go embed target (§21's "pnpm build ⇒ writes
  // static assets ⇒ Go embeds assets"), so no separate copy step is
  // needed between the frontend build and `go build`.
  build: {
    outDir: "../../internal/panel/assets/dist",
    emptyOutDir: true,
  },
});
