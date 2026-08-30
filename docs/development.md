# Local development

Punakawan does not use GitHub Actions. Local verification is authoritative.

1. Install Go 1.26, Node.js 22 or newer, and pnpm 11.15.1.
2. Run `make verify-local` from a clean checkout.
3. If protocol schemas change, run `make generate` and commit the generated Go and TypeScript output.
4. If panel source changes, run `make panel-build` and commit `internal/panel/assets/dist`.
5. If `scripts/install.sh`, `scripts/install.ps1`, or `scripts/configure-agent.sh` change, run `bash scripts/install_test.sh` (macOS) and, where `pwsh` is available, `pwsh -File scripts/install_windows_test.ps1` (Windows). The macOS suite includes a real install into a throwaway prefix, whose source checkout is then deleted, to confirm the install stays fully relocatable; set `PUNAKAWAN_SKIP_RELOCATION_TEST=1` to skip that slower part while iterating.

`make verify-local` installs dependencies, checks repository hygiene and generated protocols, formats/vets/typechecks, builds, runs unit/integration/end-to-end tests, checks that every internal package is either reachable from `./cmd/...` or explicitly allowlisted with a reason, and verifies the embedded panel bundle.

The end-to-end suite (`make e2e-test`, `go test -tags=e2e ./test/e2e/...`) proves complete delivery workflows against fake Atlassian/GitHub HTTP servers and real temporary git repositories: a full Jira-sourced delivery, an ad-hoc delivery's isolation, provider write recovery after a lost response, and the panel's live projection refresh over a real daemon transport. It opens no real daemon, writes no real data directory, and never touches this machine's actual home directory or git configuration.
