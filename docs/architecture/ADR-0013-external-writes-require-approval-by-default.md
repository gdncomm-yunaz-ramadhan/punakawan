# ADR-0013: External writes require approval by default

## Status
Accepted

## Context
Punakawan can read broadly from repositories, Jira, Confluence, and browser sessions, but external writes, Git pushes, issue transitions, page edits, and deployments carry real-world side effects and must be explicitly gated rather than performed autonomously (§2.4 Explicit side-effect boundaries; §16 Approval Model).

## Decision
External writes require approval by default.

## Consequences
Capability policy defaults Jira/Confluence writes, Git pushes, and destructive filesystem or deployment actions to `approval` (or `denied` for force-push/default-branch writes), and every approval request is recorded with operation, target, reason, preview, and resolution (§15.1 Capability policy; §16.2 Approval record; §16.3 Policy levels). This is the primary mitigation for autonomous code changes damaging repositories or external systems, working alongside worktree isolation, targeted tests, diff review, and Bagong's final review (§24 Risk: Autonomous code changes damage repositories).
