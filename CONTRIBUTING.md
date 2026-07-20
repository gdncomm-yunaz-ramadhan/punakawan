# Contributing

Punakawan is developed against the plan in
[`punakawan-go-typescript-detailed-plan.md`](./punakawan-go-typescript-detailed-plan.md).
Work is tracked in `bd` (beads); run `bd ready` to see unblocked work.

## Setup

```bash
make bootstrap
```

## Before submitting a change

```bash
make lint
make test
```

Go code: run `gofmt` and `go vet`. TypeScript code: run the workspace's
`lint` and `typecheck` scripts. Keep changes scoped to a single bd task
where possible.
