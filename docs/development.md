# Local development

Punakawan does not use GitHub Actions. Local verification is authoritative.

1. Install Go 1.26, Node.js 22 or newer, and pnpm 11.15.1.
2. Run `make verify-local` from a clean checkout.
3. If protocol schemas change, run `make generate` and commit the generated Go and TypeScript output.
4. If panel source changes, run `make panel-build` and commit `internal/panel/assets/dist`.

`make verify-local` installs dependencies, checks repository hygiene and generated protocols, formats/vets/typechecks, builds, runs unit/integration tests, and verifies the embedded panel bundle.
