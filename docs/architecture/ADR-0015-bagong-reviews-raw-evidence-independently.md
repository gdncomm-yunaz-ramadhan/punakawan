# ADR-0015: Bagong reviews raw evidence independently

## Status
Accepted

## Context
Punakawan's principle of evidence over confident prose requires that conclusions be traceable to source material rather than accepted on trust, and the final review step must guard against agent-generated summaries hiding missing or cosmetic-only implementation (§2.3 Evidence over confident prose; §8.4 Bagong).

## Decision
Bagong reviews raw evidence independently.

## Consequences
Bagong performs independent requirement review, diff review, test-evidence review, API compatibility review, migration review, and E2E flow comparison, and must receive raw evidence rather than only Petruk's summary, producing a verdict with requirement coverage, findings, test gaps, security findings, and an honest summary (§8.4). This independent review is both a named mitigation for agent-generated knowledge becoming stale or false and for autonomous code changes damaging repositories, and it gates the final delivery: a `changes_required` verdict sends Semar back to reopen or create follow-up tasks before delivery (§24 Risk: Agent-generated knowledge becomes stale or false; §24 Risk: Autonomous code changes damage repositories; §9 End-to-End Feature Workflow).
