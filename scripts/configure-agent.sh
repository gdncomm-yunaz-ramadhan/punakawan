#!/usr/bin/env bash
# Register Punakawan's generated STDIO MCP launcher with an agent client.
# Called by install.sh, but intentionally standalone so it can be rerun when
# users install another client later.
set -euo pipefail

RUN_SCRIPT="${1:-}"
if [[ -z "$RUN_SCRIPT" || ! -x "$RUN_SCRIPT" ]]; then
  echo "Usage: $0 /absolute/path/to/run-mcp.sh" >&2
  exit 2
fi

GLOBAL_DIR="$(cd "$(dirname "$RUN_SCRIPT")" && pwd)"
GENERIC_CONFIG="$GLOBAL_DIR/mcp-config.json"
DRY_RUN="${PUNAKAWAN_DRY_RUN:-0}"
RESULTS=()

log() { printf '\n==> %s\n' "$1"; }
warn() { printf 'Warning: %s\n' "$1" >&2; }
record_result() { RESULTS[${#RESULTS[@]}]="$1"; }

print_command() {
  printf '  '
  printf '%q ' "$@"
  printf '\n'
}

run_command() {
  if [[ "$DRY_RUN" == "1" ]]; then
    print_command "$@"
    return 0
  fi
  "$@"
}

find_codex() {
  if [[ -n "${PUNAKAWAN_CODEX_BIN:-}" ]]; then
    [[ -x "$PUNAKAWAN_CODEX_BIN" ]] && printf '%s\n' "$PUNAKAWAN_CODEX_BIN"
    return
  fi
  if command -v codex >/dev/null 2>&1; then
    command -v codex
    return
  fi
  local bundled="/Applications/ChatGPT.app/Contents/Resources/codex"
  [[ -x "$bundled" ]] && printf '%s\n' "$bundled"
}

find_claude() {
  if [[ -n "${PUNAKAWAN_CLAUDE_BIN:-}" ]]; then
    [[ -x "$PUNAKAWAN_CLAUDE_BIN" ]] && printf '%s\n' "$PUNAKAWAN_CLAUDE_BIN"
    return
  fi
  command -v claude 2>/dev/null || true
}

register_codex() {
  local codex_bin
  codex_bin="$(find_codex || true)"
  if [[ -z "$codex_bin" ]]; then
    warn "Codex was selected but no Codex CLI was found on PATH or in the ChatGPT app bundle."
    echo "Install Codex, then rerun:"
    echo "  scripts/configure-agent.sh \"$RUN_SCRIPT\""
    record_result "Codex: not configured (client not found)"
    return 0
  fi

  log "Registering Punakawan with Codex"
  if [[ "$DRY_RUN" == "1" ]]; then
    print_command "$codex_bin" mcp remove punakawan
  else
    "$codex_bin" mcp remove punakawan >/dev/null 2>&1 || true
  fi
  run_command "$codex_bin" mcp add punakawan -- "$RUN_SCRIPT"
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "Codex dry run complete; no configuration changed."
    record_result "Codex: dry run"
  else
    echo "Codex registration complete. Restart the Codex app, CLI, or IDE extension."
    record_result "Codex: configured"
  fi
}

register_claude() {
  local claude_bin
  claude_bin="$(find_claude || true)"
  if [[ -z "$claude_bin" ]]; then
    warn "Claude Code was selected but the claude CLI was not found on PATH."
    echo "Install Claude Code, then rerun:"
    echo "  scripts/configure-agent.sh \"$RUN_SCRIPT\""
    record_result "Claude Code: not configured (client not found)"
    return 0
  fi

  log "Registering Punakawan with Claude Code"
  if [[ "$DRY_RUN" == "1" ]]; then
    print_command "$claude_bin" mcp remove punakawan --scope user
  else
    "$claude_bin" mcp remove punakawan --scope user >/dev/null 2>&1 || true
  fi
  run_command "$claude_bin" mcp add punakawan --scope user -- "$RUN_SCRIPT"
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "Claude Code dry run complete; no configuration changed."
    record_result "Claude Code: dry run"
  else
    echo "Claude Code registration complete. Restart Claude Code."
    record_result "Claude Code: configured"
  fi
}

json_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '%s' "$value"
}

write_generic_config() {
  local escaped_run_script
  escaped_run_script="$(json_escape "$RUN_SCRIPT")"

  if [[ "$DRY_RUN" == "1" ]]; then
    log "Generic MCP configuration (would write $GENERIC_CONFIG)"
  else
    cat > "$GENERIC_CONFIG" <<JSON
{
  "mcpServers": {
    "punakawan": {
      "command": "$escaped_run_script",
      "args": []
    }
  }
}
JSON
    chmod 600 "$GENERIC_CONFIG"
    log "Wrote generic MCP configuration to $GENERIC_CONFIG"
  fi

  cat <<EOF
For any STDIO MCP client, configure:
  name:    punakawan
  command: $RUN_SCRIPT
  args:    []
EOF
  if [[ "$DRY_RUN" == "1" ]]; then
    record_result "Other MCP client: generic config dry run"
  else
    record_result "Other MCP client: wrote $GENERIC_CONFIG"
  fi
}

choose_client() {
  if [[ -n "${PUNAKAWAN_AGENT_SELECTION:-}" ]]; then
    printf '%s\n' "$PUNAKAWAN_AGENT_SELECTION"
    return
  fi

  cat >&2 <<'EOF'

Which agent client should Punakawan integrate with?
  1) Codex
  2) Claude Code
  3) Both Codex and Claude Code
  4) Another MCP client (write generic configuration)
  5) Skip for now
EOF
  local selection
  read -rp "Choose [1-5, default 3]: " selection
  printf '%s\n' "${selection:-3}"
}

SELECTION="$(choose_client)"
NORMALIZED_SELECTION="$(printf '%s' "$SELECTION" | tr '[:upper:]' '[:lower:]')"
case "$NORMALIZED_SELECTION" in
  1|codex)
    register_codex
    ;;
  2|claude|claude-code)
    register_claude
    ;;
  3|both)
    register_codex
    register_claude
    ;;
  4|other|generic)
    write_generic_config
    ;;
  5|skip|none)
    log "Skipped agent-client registration"
    echo "Rerun this wizard later with:"
    echo "  scripts/configure-agent.sh \"$RUN_SCRIPT\""
    record_result "Agent integration: skipped"
    ;;
  *)
    echo "Unknown selection $SELECTION; expected 1-5." >&2
    exit 2
    ;;
esac

log "Agent integration wizard finished"
for result in "${RESULTS[@]}"; do
  printf '  - %s\n' "$result"
done
