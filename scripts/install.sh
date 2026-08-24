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
INSTALL_DIR="${PUNAKAWAN_INSTALL_DIR:-$HOME/.local/bin}"
CONFIG_DIR="${PUNAKAWAN_CONFIG_DIR:-$HOME/Library/Application Support/punakawan}"

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
run pnpm --filter @punakawan/panel build

log "Installing punakawan and punakawand"
run mkdir -p "$INSTALL_DIR"
run env "GOBIN=$INSTALL_DIR" go install ./cmd/punakawan ./cmd/punakawand

if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
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
PUNAKAWAN_DRY_RUN="$DRY_RUN" bash "$SCRIPT_DIR/configure-agent.sh" "$INSTALL_DIR/punakawan" "$CONFIG_DIR"

cat <<EOF

==> Done.
Binary directory: $INSTALL_DIR
Generic MCP config: $CONFIG_DIR/mcp-config.json
Panel: punakawan panel --workspace /absolute/path/to/project
MCP: punakawan mcp serve
EOF
