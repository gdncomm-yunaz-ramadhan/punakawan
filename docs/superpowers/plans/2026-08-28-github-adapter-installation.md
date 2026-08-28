# GitHub Adapter Installation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Install and launch the GitHub adapter with credentials loaded from Punakawan's global `.env` file.

**Architecture:** Extend the macOS installer to build and register direct adapters. Extend the MCP client configurator to generate an environment-loading launcher and register that launcher instead of the raw binary.

**Tech Stack:** Bash, pnpm, Go installer tests.

**Spec:** `docs/superpowers/specs/2026-08-28-github-adapter-installation-design.md`

## Global Constraints

- Preserve existing user adapter configuration.
- Never print or commit credential values.
- GitHub adapter environment is allowlisted like Atlassian's.

### Task 1: Test launcher and registration

**Files:** `scripts/configure-agent.sh`, `scripts/install_test.sh`

- [x] Add installer assertions for generated launcher source line and launcher-based MCP registrations.
- [x] Run `bash scripts/install_test.sh` and observe the required failure.
- [x] Generate a launcher that sources the passed global `.env` and forwards arguments to Punakawan.
- [x] Register the launcher with Codex, Claude Code, and generic MCP config.
- [x] Run `bash scripts/install_test.sh` and confirm pass.

### Task 2: Test adapter configuration

**Files:** `scripts/install.sh`, `scripts/install_test.sh`, `README.md`

- [x] Add dry-run assertions for GitHub adapter build and configuration.
- [x] Run `bash scripts/install_test.sh` and observe the required failure.
- [x] Build both direct adapters and append only missing global adapter entries.
- [x] Pass global `.env` to the configurator and document `GITHUB_TOKEN` location.
- [x] Run installer test and relevant build checks.
