# Security Policy

Punakawan executes agent-driven code changes, holds credentials for external
systems (Jira, Confluence, GitHub/GitLab), and drives browser automation.
Treat any related report as sensitive.

## Reporting a vulnerability

Open a private security advisory on the repository, or contact the
maintainer directly. Do not open a public issue for suspected
vulnerabilities.

## Scope

Reports involving repository integrity, credential exposure, process
supervision, adapter write authorization, or the loopback panel are in
scope. Execution is not gated on user confirmation: an adapter operation
declaring `side_effect: true` is meant to route through schema validation, a
durable outbox, and an audit trail before reaching the target system, not
through a human approval step. A report that this authorization path can be
bypassed, or that a write reaches a provider without going through it, is in
scope.
