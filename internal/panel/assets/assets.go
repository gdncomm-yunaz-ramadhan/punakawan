// Package assets embeds the Punakawan Panel's built frontend into the Go
// binary, per punakawan-panel-implementation-plan.md §21: "Go embeds
// assets ⇒ Go binary built." dist/ starts out holding only a placeholder
// page (checked into git so a fresh clone still builds without running
// the frontend's build step first); `pnpm --filter @punakawan/panel
// build` overwrites it with the real Vite/Svelte output.
package assets

import "embed"

//go:embed dist
var Dist embed.FS

// DistDir is the subdirectory within Dist that holds the actual files,
// since go:embed's "dist" pattern keeps the "dist/" prefix on every path.
const DistDir = "dist"
