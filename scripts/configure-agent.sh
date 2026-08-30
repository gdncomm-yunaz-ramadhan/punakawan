#!/usr/bin/env bash
# Auto-registers the installed Punakawan binary with detected MCP clients.
set -euo pipefail

PUNAKAWAN_BIN="${1:-}"
CONFIG_DIR="${2:-}"
ENV_FILE="${3:-}"
DRY_RUN="${PUNAKAWAN_DRY_RUN:-0}"

if [[ -z "$PUNAKAWAN_BIN" || -z "$CONFIG_DIR" ]]; then
  printf 'Usage: %s /absolute/path/to/punakawan /absolute/config/directory [global-env-file]\n' "$0" >&2
  exit 2
fi
if [[ -z "$ENV_FILE" ]]; then
  ENV_FILE="$CONFIG_DIR/.env"
fi
if [[ "$DRY_RUN" != "1" && ! -x "$PUNAKAWAN_BIN" ]]; then
  printf 'Punakawan binary is not executable: %s\n' "$PUNAKAWAN_BIN" >&2
  exit 2
fi

log() {
  printf '\n==> %s\n' "$1"
}

warn() {
  printf 'WARNING: %s\n' "$1" >&2
}

print_command() {
  printf '    '
  printf '%q ' "$@"
  printf '\n'
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

manual_codex() {
  printf 'Manual setup: codex mcp add punakawan -- %q mcp serve\n' "$LAUNCHER" >&2
}

manual_claude() {
  printf 'Manual setup: claude mcp add punakawan --scope user -- %q mcp serve\n' "$LAUNCHER" >&2
}

register_codex() {
  local client="$1"
  log "Registering Punakawan with Codex"
  if [[ "$DRY_RUN" == "1" ]]; then
    print_command "$client" mcp remove punakawan
    print_command "$client" mcp add punakawan -- "$LAUNCHER" mcp serve
    return
  fi
  "$client" mcp remove punakawan >/dev/null 2>&1 || true
  if "$client" mcp add punakawan -- "$LAUNCHER" mcp serve; then
    printf 'Codex configured. Restart Codex to load Punakawan.\n'
  else
    warn "Codex registration failed. Punakawan remains installed."
    manual_codex
  fi
}

register_claude() {
  local client="$1"
  log "Registering Punakawan with Claude Code"
  if [[ "$DRY_RUN" == "1" ]]; then
    print_command "$client" mcp remove punakawan --scope user
    print_command "$client" mcp add punakawan --scope user -- "$LAUNCHER" mcp serve
    return
  fi
  "$client" mcp remove punakawan --scope user >/dev/null 2>&1 || true
  if "$client" mcp add punakawan --scope user -- "$LAUNCHER" mcp serve; then
    printf 'Claude Code configured. Restart Claude Code to load Punakawan.\n'
  else
    warn "Claude Code registration failed. Punakawan remains installed."
    manual_claude
  fi
}

json_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '%s' "$value"
}

write_generic_config() {
  local path="$CONFIG_DIR/mcp-config.json"
  local escaped_bin
  escaped_bin="$(json_escape "$LAUNCHER")"
  if [[ "$DRY_RUN" == "1" ]]; then
    log "Generic MCP config: $path"
    return
  fi
  mkdir -p "$CONFIG_DIR"
  cat >"$path" <<JSON
{
  "mcpServers": {
    "punakawan": {
      "command": "$escaped_bin",
      "args": ["mcp", "serve"]
    }
  }
}
JSON
  chmod 600 "$path"
  log "Wrote generic MCP config: $path"
}

write_launcher() {
  LAUNCHER="$CONFIG_DIR/run-mcp.sh"
  if [[ "$DRY_RUN" == "1" ]]; then
    log "MCP launcher: $LAUNCHER"
    return
  fi
  mkdir -p "$CONFIG_DIR"
  {
    printf '%s\n' '#!/usr/bin/env bash' 'set -euo pipefail'
    printf 'if [[ -f %q ]]; then\n' "$ENV_FILE"
    printf '%s\n' '  set -a'
    printf '  source %q\n' "$ENV_FILE"
    printf '%s\n' '  set +a' 'fi'
    printf 'exec %q "$@"\n' "$PUNAKAWAN_BIN"
  } >"$LAUNCHER"
  chmod 700 "$LAUNCHER"
  log "Wrote MCP launcher: $LAUNCHER"
}

write_launcher
codex_bin="$(find_codex || true)"
claude_bin="$(find_claude || true)"

if [[ -n "$codex_bin" ]]; then
  register_codex "$codex_bin"
  log "Configuring Codex lifecycle telemetry hooks"
  if [[ "$DRY_RUN" == "1" ]]; then
    print_command "$LAUNCHER" hooks install-global
  else
    "$LAUNCHER" hooks install-global || warn "Could not configure Codex lifecycle hooks; delivery usage tracking for Codex sessions will be incomplete until this is retried."
  fi
else
  warn "Codex not detected; skipping automatic registration."
  manual_codex
fi

if [[ -n "$claude_bin" ]]; then
  register_claude "$claude_bin"
else
  warn "Claude Code not detected; skipping automatic registration."
  manual_claude
fi

write_generic_config
