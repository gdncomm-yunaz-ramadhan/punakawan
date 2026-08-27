# Delivery Cost Accuracy Design

## Goal

Make delivery cost reporting truthful and inspectable without adding parallel
worklog or cost APIs.

## Current failure

`TRF-19272` has a manually reported `engineering_worklog` usage entry with
`unit_price=0`, `cost_amount=0`, and `cost_currency=USD`. That claims a known
monetary cost where none exists. The existing Claude `SubagentStop` hook records
raw token and elapsed-time usage but intentionally leaves price fields empty;
it cannot observe a root Codex session. The panel only sums `estimate` entries,
so actual cost is neither visible nor distinguishable from unknown cost.

## Design

Keep `report_delivery_usage` as the single public usage API. It continues to
record immutable raw usage. When its supplied `id` identifies an existing entry
in the same delivery session, it corrects only its price metadata
(`unit_price`, `cost_amount`, `cost_currency`, and `price_source`), preserving
the measured quantity, model, unit, kind, and timestamp. A nil price always
stores null monetary fields; zero is a valid rate only when a non-empty price
source explicitly identifies it as free.

Usage collection remains provider-observed. The Claude transcript hook records
subagent token categories and elapsed time, with no embedded pricing table.
It also reads a root transcript when a provider exposes that path in its hook
payload. Providers that do not expose usage leave the ledger unpriced; the
agent can enrich those existing rows through `report_delivery_usage` using a
current, sourced rate. No pricing is inferred from worklog duration.

The panel derives three independent values from the lifecycle ledger:

- time spent: actual rows measured in seconds;
- token spent: actual rows measured in tokens;
- monetary cost: separate estimate and actual totals by currency, plus an
  explicit unknown-cost indicator when any monetary rate is absent.

The cost metric opens an accessible dialog from an information button. The
dialog shows time, token totals, estimate totals, actual totals, and an
unknown-cost explanation. It never formats null cost as currency.

## Data repair

Migration normalizes legacy zero-dollar engineering-worklog rows whose source
states no billing rate was available: price and currency become null/empty, and
the row remains as duration evidence. The active `TRF-19272` record is repaired
by that migration.

## Compatibility and safety

Existing callers create new usage entries exactly as before. Reusing a usage ID
for correction is session-scoped and rejects changes to measured fields. Price
correction is idempotent. No public worklog, retry, or relocation tool is
added. Worklog duration remains independent from monetary usage.

## Verification

- Go tests prove null-cost storage, safe price correction, and legacy repair.
- Hook tests prove token/time collection remains unpriced until enrichment.
- Panel tests prove dialog content and unknown-cost text.
- Focused Go and panel suites pass before integration.
