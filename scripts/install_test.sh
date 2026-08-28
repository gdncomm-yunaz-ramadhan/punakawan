#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

assert_contains() {
  local output="$1"
  local expected="$2"
  [[ "$output" == *"$expected"* ]] || fail "output missing: $expected"
}

test_root="$(mktemp -d "${TMPDIR:-/tmp}/punakawan-installer.XXXXXX")"
trap 'rm -rf "$test_root"' EXIT

installed_bin="$test_root/bin/punakawan"
config_dir="$test_root/config"
env_file="$config_dir/.env"
call_log="$test_root/client-calls.log"
wrapper_log="$test_root/wrapper.log"
mkdir -p "$(dirname "$installed_bin")"
mkdir -p "$config_dir"
printf 'GITHUB_TOKEN=test-token\n' >"$env_file"

cat >"$installed_bin" <<'SCRIPT'
#!/usr/bin/env bash
if [[ -n "${PUNAKAWAN_TEST_WRAPPER_LOG:-}" ]]; then
  printf 'github=%s args=%s\n' "${GITHUB_TOKEN:-}" "$*" >"$PUNAKAWAN_TEST_WRAPPER_LOG"
fi
exit 0
SCRIPT

for client in codex claude; do
  cat >"$test_root/$client" <<'SCRIPT'
#!/usr/bin/env bash
printf '%s:%s\n' "$(basename "$0")" "$*" >>"$PUNAKAWAN_TEST_CALL_LOG"
SCRIPT
done
chmod +x "$installed_bin" "$test_root/codex" "$test_root/claude"

PUNAKAWAN_CODEX_BIN="$test_root/codex" \
PUNAKAWAN_CLAUDE_BIN="$test_root/claude" \
PUNAKAWAN_TEST_CALL_LOG="$call_log" \
  bash "$SCRIPT_DIR/configure-agent.sh" "$installed_bin" "$config_dir" "$env_file" >/dev/null

launcher="$config_dir/run-mcp.sh"
PUNAKAWAN_TEST_WRAPPER_LOG="$wrapper_log" "$launcher" mcp serve
grep -F 'github=test-token args=mcp serve' "$wrapper_log" >/dev/null || fail "launcher does not source global environment and forward MCP arguments"

for expected in \
  "codex:mcp remove punakawan" \
  "codex:mcp add punakawan -- $launcher mcp serve" \
  "claude:mcp remove punakawan --scope user" \
  "claude:mcp add punakawan --scope user -- $launcher mcp serve"
do
  grep -F "$expected" "$call_log" >/dev/null || fail "missing client call: $expected"
done

generic_config="$config_dir/mcp-config.json"
grep -F "\"command\": \"$launcher\"" "$generic_config" >/dev/null || fail "generic MCP config has wrong command"
grep -F '"args": ["mcp", "serve"]' "$generic_config" >/dev/null || fail "generic MCP config has wrong args"

cat >"$test_root/failing-codex" <<'SCRIPT'
#!/usr/bin/env bash
exit 1
SCRIPT
chmod +x "$test_root/failing-codex"
failure_output="$(
  PUNAKAWAN_CODEX_BIN="$test_root/failing-codex" \
  PUNAKAWAN_CLAUDE_BIN="$test_root/not-installed" \
    bash "$SCRIPT_DIR/configure-agent.sh" "$installed_bin" "$config_dir" "$env_file" 2>&1
)"
assert_contains "$failure_output" "codex mcp add punakawan -- $launcher mcp serve"

bash -n "$SCRIPT_DIR/install.sh"
for expected in \
  'function Write-AdapterConfig' \
  'function Write-EnvironmentFile' \
  'function Write-McpLauncher' \
  'GITHUB_TOKEN' \
  'github-adapter' \
  'run-mcp.ps1'
do
  grep -F "$expected" "$SCRIPT_DIR/install.ps1" >/dev/null || fail "Windows installer missing: $expected"
done

if [[ "$(uname -s)" == "Darwin" ]]; then
  mac_output="$(bash "$SCRIPT_DIR/install.sh" --dry-run)"
  assert_contains "$mac_output" "pnpm -r --if-present build"
  assert_contains "$mac_output" "go install ./cmd/punakawan ./cmd/punakawand"
  assert_contains "$mac_output" "Configuring global adapters"
  assert_contains "$mac_output" "punakawan panel"

  override_output="$(PUNAKAWAN_INSTALL_DIR="$test_root/override-bin" bash "$SCRIPT_DIR/install.sh" --dry-run)"
  [[ "$override_output" != *"Added $test_root/override-bin to PATH"* ]] || fail "override install edits shell PATH"
  [[ "$override_output" != *"append"* ]] || fail "override install prints shell PATH update"

  if bash "$SCRIPT_DIR/install.sh" --unknown-option >/dev/null 2>&1; then
    fail "macOS installer accepted an unknown option"
  fi
fi

powershell_command=""
if command -v pwsh >/dev/null 2>&1; then
  powershell_command="pwsh"
elif command -v powershell.exe >/dev/null 2>&1; then
  powershell_command="powershell.exe"
fi

if [[ -n "$powershell_command" ]]; then
  windows_output="$($powershell_command -NoProfile -File "$SCRIPT_DIR/install.ps1" -DryRun)"
  assert_contains "$windows_output" "pnpm -r --if-present build"
  assert_contains "$windows_output" "go install ./cmd/punakawan ./cmd/punakawand"
  assert_contains "$windows_output" "mcp add punakawan"
  assert_contains "$windows_output" "Generic MCP config"
  assert_contains "$windows_output" "Configuring global adapters"
  assert_contains "$windows_output" "punakawan panel"
fi

printf 'installer checks passed\n'
