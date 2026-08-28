#!/usr/bin/env bash
# Installs Punakawan from this checkout on macOS.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/install.sh [--dry-run]

  --dry-run  Print installation actions without changing the machine.
EOF
}

DRY_RUN=0
if [[ $# -gt 1 ]]; then
  usage >&2
  exit 2
fi
if [[ $# -eq 1 ]]; then
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
fi

if [[ "$(uname -s)" != "Darwin" ]]; then
  printf 'This installer supports macOS only. Windows: scripts/install.ps1\n' >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
INSTALL_DIR_OVERRIDDEN=0
if [[ -n "${PUNAKAWAN_INSTALL_DIR:-}" ]]; then
  INSTALL_DIR="$PUNAKAWAN_INSTALL_DIR"
  INSTALL_DIR_OVERRIDDEN=1
else
  INSTALL_DIR="$HOME/.local/bin"
fi
CONFIG_DIR="${PUNAKAWAN_CONFIG_DIR:-$HOME/Library/Application Support/punakawan}"
GLOBAL_ENV="$CONFIG_DIR/.env"
GLOBAL_CONFIG="$CONFIG_DIR/config.yaml"
ATLASSIAN_ADAPTER_ENTRY="$REPO_ROOT/packages/adapter-atlassian/dist/run.js"
GITHUB_ADAPTER_ENTRY="$REPO_ROOT/packages/github-adapter/dist/run.js"

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

run() {
  print_command "$@"
  if [[ "$DRY_RUN" -eq 0 ]]; then
    "$@"
  fi
}

ensure_adapter_entry() {
  local adapter_id="$1"
  local adapter_entry="$2"
  shift 2

  if grep -Eq "^[[:space:]]{2}${adapter_id}:[[:space:]]*(#.*)?$" "$GLOBAL_CONFIG"; then
    return
  fi

  {
    printf '  %s:\n' "$adapter_id"
    printf '    command: %s\n' "$(command -v node)"
    printf '    args:\n'
    printf '      - %s\n' "$adapter_entry"
    printf '    env_passthrough:\n'
    local env_name
    for env_name in "$@"; do
      printf '      - %s\n' "$env_name"
    done
  } >>"$GLOBAL_CONFIG"
}

configure_global_adapters() {
  log "Configuring global adapters"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    printf '    add missing atlassian and github entries to %q\n' "$GLOBAL_CONFIG"
    return
  fi

  mkdir -p "$CONFIG_DIR"
  if [[ ! -f "$GLOBAL_CONFIG" ]]; then
    printf '%s\n' 'adapters:' >"$GLOBAL_CONFIG"
  elif ! grep -Eq '^adapters:[[:space:]]*$' "$GLOBAL_CONFIG"; then
    printf 'Cannot safely add adapters to %s; expected a block-style top-level adapters key.\n' "$GLOBAL_CONFIG" >&2
    exit 1
  fi

  ensure_adapter_entry atlassian "$ATLASSIAN_ADAPTER_ENTRY" \
    ATLASSIAN_API_TOKEN ATLASSIAN_API_TOKEN_SCOPED ATLASSIAN_HOST ATLASSIAN_EMAIL
  ensure_adapter_entry github "$GITHUB_ADAPTER_ENTRY" \
    GITHUB_TOKEN GH_TOKEN GITHUB_API_URL GITHUB_GRAPHQL_URL
  chmod 600 "$GLOBAL_CONFIG"
}

configure_global_environment() {
  if [[ "$DRY_RUN" -eq 1 ]]; then
    return
  fi

  mkdir -p "$CONFIG_DIR"
  touch "$GLOBAL_ENV"
  chmod 600 "$GLOBAL_ENV"
  if [[ -n "${GITHUB_TOKEN:-}" ]] && ! grep -Eq '^(GITHUB_TOKEN|GH_TOKEN)=' "$GLOBAL_ENV"; then
    printf 'GITHUB_TOKEN=%q\n' "$GITHUB_TOKEN" >>"$GLOBAL_ENV"
  elif [[ -n "${GH_TOKEN:-}" ]] && ! grep -Eq '^(GITHUB_TOKEN|GH_TOKEN)=' "$GLOBAL_ENV"; then
    printf 'GH_TOKEN=%q\n' "$GH_TOKEN" >>"$GLOBAL_ENV"
  fi
}

manual_install() {
  local name="$1"
  local command="$2"
  local url="$3"
  warn "Could not install $name automatically."
  printf 'Manual install: %s\nDocs: %s\n' "$command" "$url" >&2
}

install_with_brew() {
  local command_name="$1"
  local formula="$2"
  local manual_command="$3"
  local docs_url="$4"

  if command -v "$command_name" >/dev/null 2>&1; then
    log "$command_name already installed"
    return 0
  fi
  if ! command -v brew >/dev/null 2>&1; then
    manual_install "$command_name" "$manual_command" "$docs_url"
    printf 'Homebrew: https://brew.sh\n' >&2
    return 1
  fi

  log "Installing $command_name with Homebrew"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    print_command brew install "$formula"
    return 0
  fi
  if brew install "$formula"; then
    hash -r
    if command -v "$command_name" >/dev/null 2>&1; then
      return 0
    fi
  fi

  manual_install "$command_name" "$manual_command" "$docs_url"
  return 1
}

prerequisite_failure=0
install_with_brew go go 'brew install go' 'https://go.dev/doc/install' || prerequisite_failure=1
install_with_brew node node 'brew install node' 'https://nodejs.org/en/download' || prerequisite_failure=1
install_with_brew pnpm pnpm 'brew install pnpm' 'https://pnpm.io/installation' || prerequisite_failure=1

if [[ "$prerequisite_failure" -ne 0 ]]; then
  exit 1
fi

log "Building Punakawan with embedded panel assets"
cd "$REPO_ROOT"
run go mod download
run pnpm install --frozen-lockfile
run pnpm -r --if-present build

log "Installing punakawan and punakawand"
run mkdir -p "$INSTALL_DIR"
run env "GOBIN=$INSTALL_DIR" go install ./cmd/punakawan ./cmd/punakawand

if [[ "$DRY_RUN" -eq 0 ]]; then
  [[ -f "$ATLASSIAN_ADAPTER_ENTRY" ]] || { printf 'Build did not produce %s\n' "$ATLASSIAN_ADAPTER_ENTRY" >&2; exit 1; }
  [[ -f "$GITHUB_ADAPTER_ENTRY" ]] || { printf 'Build did not produce %s\n' "$GITHUB_ADAPTER_ENTRY" >&2; exit 1; }
fi
configure_global_adapters
configure_global_environment

if [[ "$INSTALL_DIR_OVERRIDDEN" -eq 0 && ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
  profile=""
  case "${SHELL:-}" in
    */zsh) profile="$HOME/.zprofile" ;;
    */bash) profile="$HOME/.bash_profile" ;;
  esac

  if [[ -n "$profile" ]]; then
    path_line="export PATH=\"$INSTALL_DIR:\$PATH\""
    if [[ "$DRY_RUN" -eq 1 ]]; then
      printf '    append %q to %q\n' "$path_line" "$profile"
    elif [[ ! -f "$profile" ]] || ! grep -Fqx "$path_line" "$profile"; then
      printf '\n%s\n' "$path_line" >>"$profile"
      log "Added $INSTALL_DIR to PATH in $profile"
    fi
  else
    warn "Add $INSTALL_DIR to PATH manually."
  fi
fi

log "Verifying installation"
if [[ "$DRY_RUN" -eq 1 ]]; then
  print_command "$INSTALL_DIR/punakawan" --help
else
  [[ -x "$INSTALL_DIR/punakawan" ]] || {
    printf 'Install did not produce %s/punakawan\n' "$INSTALL_DIR" >&2
    exit 1
  }
  [[ -x "$INSTALL_DIR/punakawand" ]] || {
    printf 'Install did not produce %s/punakawand\n' "$INSTALL_DIR" >&2
    exit 1
  }
  "$INSTALL_DIR/punakawan" --help >/dev/null
fi

log "Auto-configuring detected MCP clients"
PUNAKAWAN_DRY_RUN="$DRY_RUN" bash "$SCRIPT_DIR/configure-agent.sh" "$INSTALL_DIR/punakawan" "$CONFIG_DIR" "$GLOBAL_ENV"

cat <<EOF

==> Done.
Binary directory: $INSTALL_DIR
Generic MCP config: $CONFIG_DIR/mcp-config.json
Credentials: $GLOBAL_ENV
Global adapters: $GLOBAL_CONFIG
Panel: punakawan panel --workspace /absolute/path/to/project
MCP: punakawan mcp serve
EOF
