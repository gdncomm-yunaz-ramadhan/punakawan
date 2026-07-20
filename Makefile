.PHONY: bootstrap build test test-go test-ts lint generate protocol-check integration-test package doctor

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

# Schema-driven codegen lands with the Canonical JSON Schemas task (M0).
generate:
	@echo "generate: no schemas yet (tracked under M0 Canonical JSON Schemas / Generated types)"

# Cross-language schema compatibility check lands with the protocol package (M0).
protocol-check:
	@echo "protocol-check: no protocol package yet (tracked under M0)"

integration-test:
	go test -tags=integration ./test/integration/...

package:
	go build -o dist/punakawan ./cmd/punakawan
	go build -o dist/punakawand ./cmd/punakawand

doctor:
	go version
	node --version
	pnpm --version
