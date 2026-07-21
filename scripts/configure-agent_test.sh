#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/punakawan-agent-wizard.XXXXXX")"
trap 'rm -rf "$TEST_ROOT"' EXIT

RUN_SCRIPT="$TEST_ROOT/run-mcp.sh"
CALL_LOG="$TEST_ROOT/calls.log"
CODEX_FAKE="$TEST_ROOT/codex"
CLAUDE_FAKE="$TEST_ROOT/claude"

cat > "$RUN_SCRIPT" <<'SCRIPT'
#!/usr/bin/env bash
exit 0
SCRIPT

cat > "$CODEX_FAKE" <<'SCRIPT'
#!/usr/bin/env bash
printf 'codex:%s\n' "$*" >> "$PUNAKAWAN_TEST_CALL_LOG"
SCRIPT

cat > "$CLAUDE_FAKE" <<'SCRIPT'
#!/usr/bin/env bash
printf 'claude:%s\n' "$*" >> "$PUNAKAWAN_TEST_CALL_LOG"
SCRIPT

chmod +x "$RUN_SCRIPT" "$CODEX_FAKE" "$CLAUDE_FAKE"

PUNAKAWAN_AGENT_SELECTION=both \
PUNAKAWAN_CODEX_BIN="$CODEX_FAKE" \
PUNAKAWAN_CLAUDE_BIN="$CLAUDE_FAKE" \
PUNAKAWAN_TEST_CALL_LOG="$CALL_LOG" \
  "$SCRIPT_DIR/configure-agent.sh" "$RUN_SCRIPT" >/dev/null

for expected in \
  "codex:mcp remove punakawan" \
  "codex:mcp add punakawan -- $RUN_SCRIPT" \
  "claude:mcp remove punakawan --scope user" \
  "claude:mcp add punakawan --scope user -- $RUN_SCRIPT"
do
  rg -F "$expected" "$CALL_LOG" >/dev/null
done

PUNAKAWAN_AGENT_SELECTION=generic \
  "$SCRIPT_DIR/configure-agent.sh" "$RUN_SCRIPT" >/dev/null
GENERIC_CONFIG="$TEST_ROOT/mcp-config.json"
rg -F '"punakawan"' "$GENERIC_CONFIG" >/dev/null
rg -F "\"command\": \"$RUN_SCRIPT\"" "$GENERIC_CONFIG" >/dev/null

: > "$CALL_LOG"
DRY_OUTPUT="$TEST_ROOT/dry-run.out"
PUNAKAWAN_AGENT_SELECTION=codex \
PUNAKAWAN_CODEX_BIN="$CODEX_FAKE" \
PUNAKAWAN_TEST_CALL_LOG="$CALL_LOG" \
PUNAKAWAN_DRY_RUN=1 \
  "$SCRIPT_DIR/configure-agent.sh" "$RUN_SCRIPT" > "$DRY_OUTPUT"
[[ ! -s "$CALL_LOG" ]]
rg -F 'mcp add punakawan' "$DRY_OUTPUT" >/dev/null

MISSING_OUTPUT="$TEST_ROOT/missing-client.out"
PUNAKAWAN_AGENT_SELECTION=codex \
PUNAKAWAN_CODEX_BIN="$TEST_ROOT/not-installed" \
  "$SCRIPT_DIR/configure-agent.sh" "$RUN_SCRIPT" > "$MISSING_OUTPUT" 2>&1
rg -F 'Codex was selected but no Codex CLI was found' "$MISSING_OUTPUT" >/dev/null
rg -F 'Codex: not configured (client not found)' "$MISSING_OUTPUT" >/dev/null

printf 'configure-agent wizard tests passed\n'
