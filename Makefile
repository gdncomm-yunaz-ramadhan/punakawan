.PHONY: bootstrap schema-types-build build test test-go test-ts installer-test lint generate protocol-check integration-test panel-assets-check repo-hygiene production-imports e2e-test verify-local

bootstrap:
	go mod download
	pnpm install

schema-types-build:
	pnpm --filter @punakawan/schema-types build

build: schema-types-build
	go build ./...
	pnpm -r --if-present build

test: installer-test test-go test-ts

installer-test:
	bash scripts/install_test.sh

test-go:
	go test ./...

test-ts: schema-types-build
	pnpm -r --if-present test

lint: schema-types-build
	gofmt -l $$(git ls-files '*.go') | (! grep .)
	go vet ./...
	pnpm -r --if-present lint

# Regenerates Go structs and TS interfaces/Zod validators from protocol/*.schema.json.
generate:
	go generate ./...
	pnpm -r --if-present generate

# Fails if generated code is stale relative to protocol/*.schema.json (§5.5).
protocol-check: generate
	git diff --exit-code -- pkg/protocol packages/schema-types/src/generated packages/schema-types/src/index.ts

integration-test: build
	go test -tags=integration ./test/integration/...

e2e-test: build
	go test -tags=e2e ./test/e2e/...

repo-hygiene:
	bash scripts/repo_hygiene_test.sh

# Fails, naming every unreferenced internal package, if any internal
# package has drifted out of both the production import graph rooted at
# ./cmd/... and the explicit allowlist in scripts/production_imports_test.sh.
production-imports:
	bash scripts/production_imports_test.sh

panel-assets-check: schema-types-build
	pnpm --filter @punakawan/panel build
	git diff --exit-code -- internal/panel/assets/dist

verify-local: bootstrap repo-hygiene protocol-check lint build test integration-test e2e-test production-imports panel-assets-check

package:
	go build -o dist/punakawan ./cmd/punakawan
	go build -o dist/punakawand ./cmd/punakawand

doctor:
	go version
	node --version
	pnpm --version

# Two-terminal dev loop: run this target in one terminal, then
# `pnpm --filter @punakawan/panel dev` in another. Vite proxies /api/v1.
panel-dev:
	go run ./cmd/punakawan panel --port 7331 --open-browser=false

# Builds the frontend directly into internal/panel/assets/dist (vite.config.ts's
# outDir), then rebuilds the Go binary so it embeds the fresh assets.
panel-build:
	pnpm --filter @punakawan/panel build
	go build ./cmd/punakawan

panel-test:
	go test ./internal/panel/...
	pnpm --filter @punakawan/panel test
	pnpm --filter @punakawan/panel typecheck
