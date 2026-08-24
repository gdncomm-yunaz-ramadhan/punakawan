.PHONY: bootstrap build test test-go test-ts lint generate protocol-check integration-test package doctor panel-dev panel-build panel-test

bootstrap:
	go mod download
	pnpm install

build:
	go build ./...
	pnpm -r --if-present build

test: test-go test-ts

test-go:
	go test ./...

test-ts:
	pnpm -r --if-present test

lint:
	gofmt -l . | (! grep .)
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
