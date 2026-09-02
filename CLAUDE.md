# Project Instructions for AI Agents

This file provides instructions and context for AI coding agents working on this project.

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:7510c1e2 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->


## Build & Test

_Add your build and test commands here_

```bash
# Example:
# npm install
# npm test
```

## Architecture Overview

_Add a brief overview of your project architecture_

## Conventions & Patterns

_Add your project-specific conventions here_

<!-- punakawan:begin -->
## Punakawan delivery tracking

Work in this repository is tracked as a Punakawan delivery. Do not track it
by hand in Jira comments.

- `plan_get` before planning, `plan_save` once a plan exists or changes.
- `start_delivery` with the Jira source, a `projects` array naming this
  repository and the tasks to open in it, and a `session`. Without projects
  the delivery has no lanes and cannot run; without a session nothing
  measures its tokens, cost, or tool calls. Call it again for the same
  issue when more work turns up - it adds to that delivery rather than
  starting another.
- `map_delivery_work_item` for each lane, then `log_delivery_work` with the
  lane id when that task's work is done.
- `complete_delivery_lane` to close that lane, saying what you verified and
  whether it was accepted or failed. Nothing else moves a lane out of
  runnable: skip this and the lane stays open with every verification
  dimension pending.
- `complete_delivery` at the end. It is refused while the delivery still
  has gaps - open lanes, unreported verification, requirements no lane
  covers, unsynced worklogs, unpriceable usage. Close them rather than
  passing `acknowledge_gaps`, which is for a gap you genuinely cannot
  close and records it as waived.
- `get_delivery` shows the current state, its readiness, and the ids the
  calls above need.
<!-- punakawan:end -->
