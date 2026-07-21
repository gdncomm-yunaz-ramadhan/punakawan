#!/usr/bin/env bash
# Punakawan installer: installs prerequisites, builds Punakawan once, and
# offers a wizard for registering it with Codex, Claude Code, both, or another
# STDIO MCP client. The global setup then attaches to any git-tracked project
# directory (workspace.Discover's zero-config fallback), with no per-project
# files required.
#
# Usage: scripts/install.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

log() { printf '\n==> %s\n' "$1"; }

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "This installer currently supports macOS only (detected: $(uname -s))." >&2
  echo "See README.md for manual setup steps on other platforms." >&2
  exit 1
fi

# --- 1. Prerequisites (installed once, globally, via Homebrew) --------------

if ! command -v brew >/dev/null 2>&1; then
  echo "Homebrew is required: https://brew.sh" >&2
  exit 1
fi

install_if_missing() {
  local cmd="$1" formula="$2"
  if command -v "$cmd" >/dev/null 2>&1; then
    log "$cmd already installed ($(command -v "$cmd"))"
  else
    log "Installing $formula (provides $cmd)"
    brew install "$formula"
  fi
}

install_if_missing git git
install_if_missing rg ripgrep
install_if_missing node node
install_if_missing dolt dolt
install_if_missing bd beads
install_if_missing rtk rtk

if ! command -v pnpm >/dev/null 2>&1; then
  log "Installing pnpm"
  npm install -g pnpm
else
  log "pnpm already installed ($(command -v pnpm))"
fi

if ! command -v go >/dev/null 2>&1; then
  log "Installing go"
  brew install go
else
  log "go already installed ($(command -v go))"
fi

# --- 2. Build Punakawan (once, from this checkout) --------------------------

log "Building Punakawan (go build + pnpm -r build)"
(cd "$REPO_ROOT" && make bootstrap && make build && make package)

PUNAKAWAN_BIN="$REPO_ROOT/dist/punakawan"
ADAPTER_ATLASSIAN_ENTRY="$REPO_ROOT/packages/adapter-atlassian/dist/run.js"

if [[ ! -x "$PUNAKAWAN_BIN" ]]; then
  echo "Build did not produce $PUNAKAWAN_BIN" >&2
  exit 1
fi
if [[ ! -f "$ADAPTER_ATLASSIAN_ENTRY" ]]; then
  echo "Build did not produce $ADAPTER_ATLASSIAN_ENTRY" >&2
  exit 1
fi

LOCAL_BIN="$HOME/.local/bin"
mkdir -p "$LOCAL_BIN"
ln -sf "$PUNAKAWAN_BIN" "$LOCAL_BIN/punakawan"
log "Linked $LOCAL_BIN/punakawan -> $PUNAKAWAN_BIN"
case ":$PATH:" in
  *":$LOCAL_BIN:"*) ;;
  *) echo "Note: $LOCAL_BIN is not on your PATH. Add it in your shell profile." ;;
esac

# --- 3. Global config location (matches Go's os.UserConfigDir on macOS) -----

GLOBAL_DIR="$HOME/Library/Application Support/punakawan"
mkdir -p "$GLOBAL_DIR"
GLOBAL_CONFIG="$GLOBAL_DIR/config.yaml"
GLOBAL_ENV="$GLOBAL_DIR/.env"

# --- 4. Atlassian credentials (written once, globally) ----------------------

if [[ -f "$GLOBAL_ENV" ]]; then
  log "$GLOBAL_ENV already exists, leaving credentials as-is"
else
  log "Direct Jira REST connection"
  cat <<'EOF'
Punakawan calls Jira Cloud REST API v3 directly. Rovo MCP is not used.

Choose the token type you created:
  1) Personal API token without scopes (email + site URL)
  2) Personal API token with scopes (email + Atlassian API gateway)
  3) Service-account scoped token (Bearer + Atlassian API gateway)

Scoped Jira tokens should include read:jira-work and write:jira-work. The
account itself still needs the corresponding Jira project permissions.
Confluence reads additionally require Confluence product access/scopes.
EOF
  read -rp "Which do you have? [1/2/3, default 1]: " AUTH_CHOICE
  AUTH_CHOICE="${AUTH_CHOICE:-1}"
  if [[ ! "$AUTH_CHOICE" =~ ^[123]$ ]]; then
    echo "Invalid token choice: $AUTH_CHOICE" >&2
    exit 1
  fi

  read -rp "Atlassian site host (e.g. yourteam.atlassian.net): " ATLASSIAN_HOST_INPUT
  if command -v curl >/dev/null 2>&1; then
    TENANT_INFO="$(curl -fsS "https://${ATLASSIAN_HOST_INPUT}/_edge/tenant_info" 2>/dev/null || true)"
    if [[ "$TENANT_INFO" == *cloudId* ]]; then
      log "Resolved $ATLASSIAN_HOST_INPUT -> $TENANT_INFO"
    else
      echo "Warning: could not confirm $ATLASSIAN_HOST_INPUT resolves to a cloud ID - double-check the hostname." >&2
    fi
  fi
  read -rsp "Atlassian API token: " API_TOKEN
  echo ""

  EMAIL=""
  if [[ "$AUTH_CHOICE" != "3" ]]; then
    read -rp "Atlassian account email: " EMAIL
  fi

  TOKEN_SCOPED="false"
  if [[ "$AUTH_CHOICE" != "1" ]]; then
    TOKEN_SCOPED="true"
  fi

  {
    echo "ATLASSIAN_API_TOKEN=${API_TOKEN}"
    echo "ATLASSIAN_API_TOKEN_SCOPED=${TOKEN_SCOPED}"
    echo "ATLASSIAN_HOST=${ATLASSIAN_HOST_INPUT}"
    if [[ -n "$EMAIL" ]]; then
      echo "ATLASSIAN_EMAIL=${EMAIL}"
    fi
  } > "$GLOBAL_ENV"
  chmod 600 "$GLOBAL_ENV"
  log "Wrote credentials to $GLOBAL_ENV (chmod 600, outside any git-tracked directory)"
fi

# --- 5. Global adapter config (workspace.GlobalConfig) ----------------------

if [[ -f "$GLOBAL_CONFIG" ]]; then
  log "$GLOBAL_CONFIG already exists, leaving it as-is"
else
  cat > "$GLOBAL_CONFIG" <<YAML
adapters:
  atlassian:
    command: node
    args:
      - ${ADAPTER_ATLASSIAN_ENTRY}
    env_passthrough:
      - ATLASSIAN_API_TOKEN
      - ATLASSIAN_API_TOKEN_SCOPED
      - ATLASSIAN_HOST
      - ATLASSIAN_EMAIL
YAML
  log "Wrote $GLOBAL_CONFIG"
fi
echo "Any project can still add its own .punakawan/workspace.yaml with an"
echo "adapters: section to override this - that remains fully optional."

# --- 6. Wrapper script + agent-client integration wizard --------------------

RUN_SCRIPT="$GLOBAL_DIR/run-mcp.sh"
cat > "$RUN_SCRIPT" <<SCRIPT
#!/usr/bin/env bash
# Generated by scripts/install.sh - sources global credentials, then execs
# punakawan's MCP server from the caller's own working directory, so
# workspace.Discover resolves whichever project the agent client is using.
set -euo pipefail
if [[ -f "$GLOBAL_ENV" ]]; then
  set -a
  source "$GLOBAL_ENV"
  set +a
fi
exec "${PUNAKAWAN_BIN}" mcp serve
SCRIPT
chmod +x "$RUN_SCRIPT"
log "Wrote $RUN_SCRIPT"

"$SCRIPT_DIR/configure-agent.sh" "$RUN_SCRIPT"

# --- 7. Verify ----------------------------------------------------------------

log "Running punakawan doctor"
"$PUNAKAWAN_BIN" doctor || echo "doctor reported issues above - resolve before using punakawan"

cat <<EOF

==> Done.

Binary:        $LOCAL_BIN/punakawan -> $PUNAKAWAN_BIN
Credentials:   $GLOBAL_ENV (not git-tracked)
Global config: $GLOBAL_CONFIG
MCP launcher:  $RUN_SCRIPT

Open the agent client selected in the wizard in any git-tracked project, then
confirm that the "punakawan" MCP server is connected.

Write actions (Jira comments, transitions, subtasks, estimates) ask for one
inline human approval per run when the MCP client supports it. For
clients without elicitation support, use the CLI fallback shown in the tool
error:
  punakawan approvals list
  punakawan approvals approve <id> --by <your-name>
EOF
