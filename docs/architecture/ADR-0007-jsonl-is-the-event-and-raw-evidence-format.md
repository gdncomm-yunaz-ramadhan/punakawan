# ADR-0007: JSONL is the event and raw evidence format

## Status
Accepted

## Context
Punakawan needs an append-only, easily inspected format for run event journals, adapter events, raw browser recordings, model invocation metadata, and command execution records, distinct from the canonical relational knowledge graph held in Dolt (§7.5 JSONL use; §7 Durable Knowledge Model).

## Decision
JSONL is the event and raw evidence format.

## Consequences
JSONL is used for run event journals, adapter events, raw browser recordings, model invocation metadata, command execution records, recovery checkpoints, and import/export, but it is explicitly not the canonical relation graph — that role belongs to Dolt (§7.5). This format also underlies the evidence bundle produced for every task completion (`commands.jsonl`, plus other JSONL-friendly artifacts) and the durable event journal the Go core owns for recovery after interrupted runs (§17.2 Evidence bundle; §3.1 Go responsibilities; §18.2 Recovery).
