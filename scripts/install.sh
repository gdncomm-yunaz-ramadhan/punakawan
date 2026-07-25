#!/usr/bin/env bash
# Punakawan installer for macOS and Linux: installs prerequisites, builds
# Punakawan once, and offers a wizard for registering it with Codex, Claude
# Code, both, or another STDIO MCP client. The global setup then attaches to
# any git-tracked project directory (workspace.Discover's zero-config
# fallback), with no per-project files required.
#
# Windows users: use scripts/install.ps1 instead.
#
# Usage: scripts/install.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

log() { printf '\n==> %s\n' "$1"; }
warn() { printf '\nWARNING: %s\n' "$1" >&2; }

# --- 0. Platform detection --------------------------------------------------
# macOS installs via Homebrew; Linux uses the distro package manager (apt /
# dnf / yum / pacman / zypper) with tool-specific fallbacks for anything not
# packaged. Windows is handled by scripts/install.ps1.

OS="$(uname -s)"
case "$OS" in
  Darwin) PLATFORM="macos" ;;
  Linux) PLATFORM="linux" ;;
  *)
    echo "Unsupported OS: $OS." >&2
    echo "macOS and Linux use this script; Windows uses scripts/install.ps1." >&2
    exit 1
    ;;
esac
log "Detected platform: $PLATFORM ($OS)"

# Package-manager plumbing (Linux only; macOS uses brew directly).
PKG_MGR=""
SUDO=""
APT_UPDATED=0

detect_linux_pkg_mgr() {
  if command -v apt-get >/dev/null 2>&1; then
    PKG_MGR="apt"
  elif command -v dnf >/dev/null 2>&1; then
    PKG_MGR="dnf"
  elif command -v yum >/dev/null 2>&1; then
    PKG_MGR="yum"
  elif command -v pacman >/dev/null 2>&1; then
    PKG_MGR="pacman"
  elif command -v zypper >/dev/null 2>&1; then
    PKG_MGR="zypper"
  fi
  # System package managers need root; use sudo when we are not already root.
  if [[ "$(id -u)" -ne 0 ]] && command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
  fi
}

# Map a logical tool name to the package name for the detected manager. Most
# packages share a name; the exceptions (node, go) are spelled out.
linux_pkg_name() {
  local tool="$1"
  case "$tool:$PKG_MGR" in
    node:apt | node:dnf | node:yum | node:zypper | node:pacman) echo "nodejs" ;;
    go:apt) echo "golang-go" ;;
    go:dnf | go:yum) echo "golang" ;;
    go:pacman | go:zypper) echo "go" ;;
    *) echo "$tool" ;;
  esac
}

linux_pkg_install() {
  local pkg="$1"
  case "$PKG_MGR" in
    apt)
      if [[ "$APT_UPDATED" -eq 0 ]]; then
        $SUDO apt-get update -y
        APT_UPDATED=1
      fi
      $SUDO apt-get install -y "$pkg"
      ;;
    dnf) $SUDO dnf install -y "$pkg" ;;
    yum) $SUDO yum install -y "$pkg" ;;
    pacman) $SUDO pacman -S --needed --noconfirm "$pkg" ;;
    zypper) $SUDO zypper install -y "$pkg" ;;
    *) return 1 ;;
  esac
}

# --- 1. Prerequisites -------------------------------------------------------

if [[ "$PLATFORM" == "macos" ]]; then
  if ! command -v brew >/dev/null 2>&1; then
    echo "Homebrew is required: https://brew.sh" >&2
    exit 1
  fi
else
  detect_linux_pkg_mgr
  if [[ -z "$PKG_MGR" ]]; then
    warn "No supported package manager found (apt/dnf/yum/pacman/zypper)."
    echo "Install the prerequisites manually, then rerun: git, ripgrep, node, go, dolt, bd." >&2
  else
    log "Using package manager: $PKG_MGR${SUDO:+ (via sudo)}"
  fi
fi

# install_if_missing <command> <brew_formula> [linux_tool]
# linux_tool defaults to <command>; it is the logical name fed to
# linux_pkg_name(). A missing package manager (Linux) is a non-fatal warning
# so the rest of the setup can still proceed for tools already present.
install_if_missing() {
  local cmd="$1" brew_formula="$2" linux_tool="${3:-$1}"
  if command -v "$cmd" >/dev/null 2>&1; then
    log "$cmd already installed ($(command -v "$cmd"))"
    return 0
  fi
  if [[ "${PUNAKAWAN_DRY_RUN:-0}" == "1" ]]; then
    log "[dry-run] would install $cmd"
    return 0
  fi
  if [[ "$PLATFORM" == "macos" ]]; then
    log "Installing $brew_formula (provides $cmd)"
    brew install "$brew_formula"
  else
    if [[ -z "$PKG_MGR" ]]; then
      warn "Cannot install $cmd automatically (no package manager). Install it manually."
      return 1
    fi
    local pkg
    pkg="$(linux_pkg_name "$linux_tool")"
    log "Installing $pkg (provides $cmd) via $PKG_MGR"
    linux_pkg_install "$pkg"
  fi
}

install_if_missing git git
install_if_missing curl curl
install_if_missing rg ripgrep ripgrep
install_if_missing node node node
install_if_missing go go go

# dolt: Homebrew formula on macOS; official install script on Linux (not in
# distro repos). Fatal on macOS, best-effort with a clear pointer on Linux.
install_dolt() {
  if command -v dolt >/dev/null 2>&1; then
    log "dolt already installed ($(command -v dolt))"
    return 0
  fi
  if [[ "${PUNAKAWAN_DRY_RUN:-0}" == "1" ]]; then
    log "[dry-run] would install dolt"
    return 0
  fi
  if [[ "$PLATFORM" == "macos" ]]; then
    log "Installing dolt"
    brew install dolt
  else
    log "Installing dolt via the official install script"
    if ! curl -L https://github.com/dolthub/dolt/releases/latest/download/install.sh | $SUDO bash; then
      warn "dolt install failed. Install it manually: https://docs.dolthub.com/introduction/installation"
    fi
  fi
}
install_dolt

# bd (beads): Homebrew formula on macOS. On Linux it is not in distro repos;
# try `go install` and fall back to a clear pointer. Non-fatal - Punakawan's
# beads-backed features degrade gracefully (health reports bd unavailable).
install_beads() {
  if command -v bd >/dev/null 2>&1; then
    log "bd already installed ($(command -v bd))"
    return 0
  fi
  if [[ "${PUNAKAWAN_DRY_RUN:-0}" == "1" ]]; then
    log "[dry-run] would install bd (beads)"
    return 0
  fi
  if [[ "$PLATFORM" == "macos" ]]; then
    log "Installing beads (provides bd)"
    brew install beads || warn "beads install failed. See https://github.com/gastownhall/beads"
  else
    log "Installing beads (provides bd) via go install"
    if command -v go >/dev/null 2>&1 && GOBIN="$HOME/.local/bin" go install github.com/gastownhall/beads/cmd/bd@latest 2>/dev/null; then
      log "Installed bd -> $HOME/.local/bin/bd"
    else
      warn "Could not auto-install bd. Install beads manually: https://github.com/gastownhall/beads"
    fi
  fi
}
install_beads

# rtk (Rust Token Killer): optional token-compression proxy. macOS installs
# the brew formula; on Linux there is no unambiguous package (the name collides
# with an unrelated crate), so we only point at the source. Never fatal.
if command -v rtk >/dev/null 2>&1; then
  log "rtk already installed ($(command -v rtk))"
elif [[ "${PUNAKAWAN_DRY_RUN:-0}" == "1" ]]; then
  log "[dry-run] would install rtk"
elif [[ "$PLATFORM" == "macos" ]]; then
  log "Installing rtk"
  brew install rtk || warn "rtk install failed - optional, Punakawan works without it."
else
  warn "rtk is optional and not auto-installed on Linux; install it manually if you use it."
fi

if ! command -v pnpm >/dev/null 2>&1; then
  if [[ "${PUNAKAWAN_DRY_RUN:-0}" == "1" ]]; then
    log "[dry-run] would install pnpm"
  else
    log "Installing pnpm"
    npm install -g pnpm
  fi
else
  log "pnpm already installed ($(command -v pnpm))"
fi

# --- 1b. Optional security scanners (Sonar / CVE / OSV) ---------------------
# Punakawan's knowledge search indexes CVE, GHSA, and Sonar-rule identifiers
# as first-class tokens (internal/search/identifiers.go), and the architecture
# roadmap ingests findings from these scanners as evidence. Installing them is
# OPTIONAL and never fatal: a failed scanner install warns and continues.
#
# Control non-interactively with PUNAKAWAN_INSTALL_SCANNERS=yes|no (default:
# prompt). PUNAKAWAN_DRY_RUN=1 prints the install commands without running them.

SCANNER_CHOICE="${PUNAKAWAN_INSTALL_SCANNERS:-}"
if [[ -z "$SCANNER_CHOICE" ]]; then
  cat <<'EOF'

Optional: install security/quality scanners whose output Punakawan can index
(CVE/GHSA/Sonar-rule identifiers become searchable knowledge):
  - trivy         (container/dependency CVE scanning)
  - osv-scanner   (OSV/GHSA vulnerability scanning)
  - sonar-scanner (SonarQube/SonarCloud static analysis)
EOF
  read -rp "Install these optional scanners now? [y/N]: " SCANNER_REPLY
  case "${SCANNER_REPLY:-N}" in
    y | Y | yes | YES) SCANNER_CHOICE="yes" ;;
    *) SCANNER_CHOICE="no" ;;
  esac
fi

install_scanner() {
  local cmd="$1" brew_formula="$2"
  if command -v "$cmd" >/dev/null 2>&1; then
    log "$cmd already installed ($(command -v "$cmd"))"
    return 0
  fi
  if [[ "${PUNAKAWAN_DRY_RUN:-0}" == "1" ]]; then
    if [[ "$PLATFORM" == "macos" ]]; then
      log "[dry-run] brew install $brew_formula (provides $cmd)"
    else
      log "[dry-run] install $cmd via $PKG_MGR (provides $cmd)"
    fi
    return 0
  fi
  log "Installing $brew_formula (provides $cmd)"
  if [[ "$PLATFORM" == "macos" ]]; then
    brew install "$brew_formula" ||
      echo "Warning: failed to install $brew_formula - skipping (Punakawan still works without it)." >&2
  else
    if [[ -z "$PKG_MGR" ]] || ! linux_pkg_install "$cmd"; then
      echo "Warning: could not install $cmd via $PKG_MGR - skipping (Punakawan still works without it)." >&2
      echo "         See the scanner's own docs for a Linux binary if you need it." >&2
    fi
  fi
}

if [[ "$SCANNER_CHOICE" == "yes" ]]; then
  install_scanner trivy trivy
  install_scanner osv-scanner osv-scanner
  install_scanner sonar-scanner sonar-scanner
else
  log "Skipping optional scanners (set PUNAKAWAN_INSTALL_SCANNERS=yes to install later)"
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

# --- 3. Global config location (matches Go's os.UserConfigDir) --------------
# os.UserConfigDir() resolves to ~/Library/Application Support on macOS and
# $XDG_CONFIG_HOME (or ~/.config) on Linux - mirror that here so the installer
# writes where the binary reads.

if [[ "$PLATFORM" == "macos" ]]; then
  GLOBAL_DIR="$HOME/Library/Application Support/punakawan"
else
  GLOBAL_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/punakawan"
fi
mkdir -p "$GLOBAL_DIR"
GLOBAL_CONFIG="$GLOBAL_DIR/config.yaml"
GLOBAL_ENV="$GLOBAL_DIR/.env"

# --- 4. Atlassian credentials (written once, globally) ----------------------

if [[ -f "$GLOBAL_ENV" ]]; then
  if grep -q '^ATLASSIAN_MCP_TOKEN=' "$GLOBAL_ENV" && ! grep -q '^ATLASSIAN_API_TOKEN=' "$GLOBAL_ENV"; then
    cp -f "$GLOBAL_ENV" "$GLOBAL_ENV.before-direct-rest"
    MIGRATED_ENV="$(mktemp "$GLOBAL_DIR/.env.migrate.XXXXXX")"
    sed 's/^ATLASSIAN_MCP_TOKEN=/ATLASSIAN_API_TOKEN=/' "$GLOBAL_ENV" > "$MIGRATED_ENV"
    if grep -q '^ATLASSIAN_EMAIL=' "$GLOBAL_ENV"; then
      printf '%s\n' 'ATLASSIAN_API_TOKEN_SCOPED=false' >> "$MIGRATED_ENV"
    else
      printf '%s\n' 'ATLASSIAN_API_TOKEN_SCOPED=true' >> "$MIGRATED_ENV"
    fi
    chmod 600 "$MIGRATED_ENV"
    mv -f "$MIGRATED_ENV" "$GLOBAL_ENV"
    log "Migrated legacy token key in $GLOBAL_ENV (backup: $GLOBAL_ENV.before-direct-rest)"
  else
    log "$GLOBAL_ENV already exists, leaving credentials as-is"
  fi
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
  if grep -q 'ATLASSIAN_MCP_TOKEN' "$GLOBAL_CONFIG"; then
    cp -f "$GLOBAL_CONFIG" "$GLOBAL_CONFIG.before-direct-rest"
    MIGRATED_CONFIG="$(mktemp "$GLOBAL_DIR/config.yaml.migrate.XXXXXX")"
    awk '
      {
        gsub(/ATLASSIAN_MCP_TOKEN/, "ATLASSIAN_API_TOKEN")
        print
        if ($0 ~ /^[[:space:]]*- ATLASSIAN_API_TOKEN$/) {
          match($0, /^[[:space:]]*/)
          indent = substr($0, 1, RLENGTH)
          print indent "- ATLASSIAN_API_TOKEN_SCOPED"
        }
      }
    ' "$GLOBAL_CONFIG" > "$MIGRATED_CONFIG"
    mv -f "$MIGRATED_CONFIG" "$GLOBAL_CONFIG"
    log "Migrated direct REST environment passthrough in $GLOBAL_CONFIG (backup: $GLOBAL_CONFIG.before-direct-rest)"
  else
    log "$GLOBAL_CONFIG already exists, leaving it as-is"
  fi
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
