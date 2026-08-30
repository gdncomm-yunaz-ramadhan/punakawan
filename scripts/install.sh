#!/usr/bin/env bash
# Installs Punakawan from this checkout on macOS.
set -euo pipefail
shopt -s nullglob

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
# CONFIG_DIR is the exact directory internal/storage.DataDir resolves
# (${PUNAKAWAN_DATA_DIR} if set, else the platform config dir): every
# installed, machine-wide state - the storage kernel, the adapter trust
# file, the telemetry spool, and everything this installer writes below -
# lives under one directory so a relocated/overridden prefix cannot
# diverge between what the installer wrote and what the built binary
# resolves at runtime. PUNAKAWAN_CONFIG_DIR is accepted as a deprecated
# alias for one release.
CONFIG_DIR="${PUNAKAWAN_DATA_DIR:-${PUNAKAWAN_CONFIG_DIR:-$HOME/Library/Application Support/punakawan}}"
GLOBAL_ENV="$CONFIG_DIR/.env"
GLOBAL_CONFIG="$CONFIG_DIR/config.yaml"
ADAPTERS_DIR="$CONFIG_DIR/adapters"
ATLASSIAN_ADAPTER_ENTRY="$ADAPTERS_DIR/atlassian/dist/run.js"
GITHUB_ADAPTER_ENTRY="$ADAPTERS_DIR/github/dist/run.js"

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

# remove_adapter_entry deletes any existing top-level "  <adapter_id>:"
# block from $GLOBAL_CONFIG (the block itself plus every more-indented
# line under it), leaving every other adapter's block - one this
# installer does not own - untouched. This is what lets
# ensure_adapter_entry below unconditionally replace its own block on
# every run instead of only ever adding one once, so a later install that
# deploys a new adapter version actually updates the recorded entrypoint
# path rather than leaving a stale one referencing a version this run just
# deleted.
remove_adapter_entry() {
  local adapter_id="$1"
  [[ -f "$GLOBAL_CONFIG" ]] || return 0
  awk -v id="$adapter_id" '
    BEGIN { skip = 0 }
    $0 ~ "^  " id ":[[:space:]]*(#.*)?$" { skip = 1; next }
    skip && $0 ~ "^  [A-Za-z0-9_-]+:[[:space:]]*(#.*)?$" { skip = 0 }
    !skip { print }
  ' "$GLOBAL_CONFIG" >"$GLOBAL_CONFIG.tmp"
  mv "$GLOBAL_CONFIG.tmp" "$GLOBAL_CONFIG"
}

ensure_adapter_entry() {
  local adapter_id="$1"
  local adapter_entry="$2"
  shift 2

  remove_adapter_entry "$adapter_id"
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

# deploy_adapter installs one @punakawan workspace package's built runtime
# files plus its production dependencies below "$ADAPTERS_DIR/$slug" -
# never inside this checkout - using pnpm's own workspace deploy support
# so relative workspace:* dependencies (adapter-sdk, schema-types) resolve
# to real copied files rather than symlinks back into $REPO_ROOT. The new
# version is deployed to its own versioned directory first, then made
# current by atomically repointing the stable "$ADAPTERS_DIR/$slug"
# symlink at it (a single rename, so a launcher reading through that
# symlink mid-install never observes a half-written adapter), and only
# then are older versioned directories removed.
deploy_adapter() {
  local package_name="$1"
  local slug="$2"
  local target="$ADAPTERS_DIR/$slug"

  if [[ "$DRY_RUN" -eq 1 ]]; then
    printf '    pnpm --filter %q deploy --prod --legacy %q (atomically replacing %q)\n' "$package_name" "$target.<version>" "$target"
    return
  fi

  mkdir -p "$ADAPTERS_DIR"
  local stamp
  stamp="$(date +%Y%m%d%H%M%S)-$$"
  local versioned_dir="$target.$stamp"
  rm -rf "$versioned_dir"
  ( cd "$REPO_ROOT" && pnpm --filter "$package_name" deploy --prod --legacy "$versioned_dir" >/dev/null )

  # ln -sfn replaces an existing symlink (or nothing at all) at $target in
  # one command, on both GNU and BSD (macOS) ln: -n stops -f's unlink from
  # instead resolving through the old symlink into the directory it
  # pointed at, which is what makes this replace the symlink itself
  # rather than moving a new entry into the versioned directory it used to
  # name.
  ln -sfn "$(basename "$versioned_dir")" "$target"

  local existing
  for existing in "$ADAPTERS_DIR/$slug".*; do
    [[ -e "$existing" ]] || continue
    [[ "$existing" == "$versioned_dir" ]] && continue
    rm -rf "$existing"
  done
}

configure_global_adapters() {
  log "Configuring global adapters"
  if [[ "$DRY_RUN" -eq 1 ]]; then
    deploy_adapter "@punakawan/adapter-atlassian" atlassian
    deploy_adapter "@punakawan/github-adapter" github
    printf '    replace atlassian and github entries in %q\n' "$GLOBAL_CONFIG"
    return
  fi

  deploy_adapter "@punakawan/adapter-atlassian" atlassian
  deploy_adapter "@punakawan/github-adapter" github

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
# Adapters (and the workspace packages they depend on) build in this exact
# order before anything deploys them, so deploy_adapter always packages a
# freshly built dist/, never a stale one left from an earlier checkout.
run pnpm --filter @punakawan/schema-types build
run pnpm --filter @punakawan/adapter-sdk build
run pnpm --filter @punakawan/adapter-atlassian build
run pnpm --filter @punakawan/github-adapter build
run pnpm -r --if-present build

log "Installing punakawan and punakawand"
run mkdir -p "$INSTALL_DIR"
run env "GOBIN=$INSTALL_DIR" go install ./cmd/punakawan ./cmd/punakawand

configure_global_adapters
configure_global_environment

if [[ "$DRY_RUN" -eq 0 ]]; then
  [[ -f "$ATLASSIAN_ADAPTER_ENTRY" ]] || { printf 'Deploy did not produce %s\n' "$ATLASSIAN_ADAPTER_ENTRY" >&2; exit 1; }
  [[ -f "$GITHUB_ADAPTER_ENTRY" ]] || { printf 'Deploy did not produce %s\n' "$GITHUB_ADAPTER_ENTRY" >&2; exit 1; }
fi

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
