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

VERBOSE="${PUNAKAWAN_VERBOSE:-0}"

log() {
  printf '\n==> %s\n' "$1"
}

# ok matches install.sh's indented per-step fact, so a run driven by the
# installer reads as one sequence rather than two scripts taking turns.
ok() {
  printf '      %s %s\n' "${PUNAKAWAN_OK_GLYPH:-✓}" "$1"
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
  printf '        If you use Codex, run: codex mcp add punakawan -- %q mcp serve\n' "$LAUNCHER" >&2
}

manual_claude() {
  printf '        If you use Claude Code, run: claude mcp add punakawan --scope user -- %q mcp serve\n' "$LAUNCHER" >&2
}

register_codex() {
  local client="$1"
  if [[ "$DRY_RUN" == "1" ]]; then
    print_command "$client" mcp remove punakawan
    print_command "$client" mcp add punakawan -- "$LAUNCHER" mcp serve
    return
  fi
  "$client" mcp remove punakawan >/dev/null 2>&1 || true
  local output
  if output="$("$client" mcp add punakawan -- "$LAUNCHER" mcp serve 2>&1)"; then
    if [[ "$VERBOSE" == "1" ]]; then printf '%s\n' "$output"; fi
    ok "Codex ready (restart Codex to load Punakawan)"
  else
    printf '%s\n' "$output" >&2
    warn "Could not register with Codex. Punakawan is still installed."
    manual_codex
  fi
}

register_claude() {
  local client="$1"
  if [[ "$DRY_RUN" == "1" ]]; then
    print_command "$client" mcp remove punakawan --scope user
    print_command "$client" mcp add punakawan --scope user -- "$LAUNCHER" mcp serve
    return
  fi
  "$client" mcp remove punakawan --scope user >/dev/null 2>&1 || true
  local output
  if output="$("$client" mcp add punakawan --scope user -- "$LAUNCHER" mcp serve 2>&1)"; then
    if [[ "$VERBOSE" == "1" ]]; then printf '%s\n' "$output"; fi
    ok "Claude Code ready (restart it to load Punakawan)"
  else
    printf '%s\n' "$output" >&2
    warn "Could not register with Claude Code. Punakawan is still installed."
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
    print_command "write" "$path"
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
  if [[ "$VERBOSE" == "1" ]]; then
    ok "config for other MCP clients: $path"
  fi
}

write_launcher() {
  LAUNCHER="$CONFIG_DIR/run-mcp.sh"
  if [[ "$DRY_RUN" == "1" ]]; then
    print_command "write" "$LAUNCHER"
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
  if [[ "$VERBOSE" == "1" ]]; then
    ok "launcher: $LAUNCHER"
  fi
}

write_launcher
codex_bin="$(find_codex || true)"
claude_bin="$(find_claude || true)"

if [[ -n "$codex_bin" ]]; then
  register_codex "$codex_bin"
else
  ok "Codex not detected, skipped"
  manual_codex
fi

if [[ -n "$claude_bin" ]]; then
  register_claude "$claude_bin"
else
  ok "Claude Code not detected, skipped"
  manual_claude
fi

# User-level hooks are installed regardless of which client binaries were
# actually detected above: the hook config files (~/.codex/hooks.json,
# ~/.claude/settings.json) are independent of whether that client's CLI
# happens to be on PATH right now, and a client installed later still
# benefits from telemetry being wired up already. PUNAKAWAN_SKIP_HOOKS=1
# opts out entirely - only used by this repo's own installer test, which
# verifies hook installation separately against an isolated $HOME instead
# of ever touching this machine's real one from an ordinary install run.
if [[ "${PUNAKAWAN_SKIP_HOOKS:-0}" != "1" ]]; then
  if [[ "$DRY_RUN" == "1" ]]; then
    print_command "$LAUNCHER" setup --hooks-only
  else
    hooks_output=""
    if hooks_output="$("$LAUNCHER" setup --hooks-only 2>&1)"; then
      if [[ "$VERBOSE" == "1" ]]; then printf '%s\n' "$hooks_output"; fi
      ok "usage tracking wired into both tools"
    else
      printf '%s\n' "$hooks_output" >&2
      warn "Could not set up usage tracking; token and cost figures will be incomplete. Retry with: punakawan setup"
    fi
  fi
fi

write_generic_config
