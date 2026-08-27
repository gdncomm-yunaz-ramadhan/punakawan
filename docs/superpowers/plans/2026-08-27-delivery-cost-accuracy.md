# Delivery Cost Accuracy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Correct unknown monetary costs, support rate enrichment through the existing usage API, and expose cost evidence in the panel.

**Architecture:** Usage entries retain observed quantities. `report_delivery_usage` corrects price metadata for a prior session-owned row; hooks record raw provider usage only. Panel derives display totals from raw ledger rows and opens a dialog for details.

**Tech Stack:** Go, SQLite migrations, Svelte 5, Vitest.

**Spec:** `docs/superpowers/specs/2026-08-27-delivery-cost-accuracy-design.md`

## Tasks

### Task 1: Usage price correction

Files: `internal/delivery/lifecycle.go`, `internal/delivery/lifecycle_test.go`, `internal/mcpserver/tools_lifecycle.go`, `internal/mcpserver/tools_lifecycle_test.go`.

1. Write failing tests for null price/currency, correction of an existing session-owned entry, and rejection of cross-session correction.
2. Implement nullable cost serialization and idempotent price-only correction behind existing `report_delivery_usage`.
3. Run `go test ./internal/delivery ./internal/mcpserver`.

### Task 2: Usage capture and legacy repair

Files: `cmd/punakawan/hooks_cmd.go`, `cmd/punakawan/hooks_cmd_test.go`, `internal/storage/migrations/0025_normalize_unknown_usage_costs.sql`.

1. Write failing test that a transcript payload with no subagent id still records observed usage.
2. Remove the subagent-only guard while retaining missing-transcript safety.
3. Add idempotent migration clearing legacy no-rate USD-zero worklog entries.
4. Run `go test ./cmd/punakawan ./internal/transcriptusage`.

### Task 3: Cost detail panel

Files: `web/panel/src/routes/deliveries/DeliveryDetail.svelte`, `web/panel/tests/DeliveryDetail.test.ts`.

1. Write failing test for information button/dialog and detailed time/token/cost rows.
2. Implement accessible dialog and null-safe cost totals.
3. Run `npm test -- DeliveryDetail.test.ts` from `web/panel`.

### Task 4: Verification and delivery repair

1. Run focused Go/panel suites and inspect diff.
2. Run migration against runtime database, then verify `TRF-19272` has no false USD-zero cost.
3. Commit and push.
