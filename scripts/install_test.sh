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
call_log="$test_root/client-calls.log"
mkdir -p "$(dirname "$installed_bin")"

cat >"$installed_bin" <<'SCRIPT'
#!/usr/bin/env bash
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
  bash "$SCRIPT_DIR/configure-agent.sh" "$installed_bin" "$config_dir" >/dev/null

for expected in \
  "codex:mcp remove punakawan" \
  "codex:mcp add punakawan -- $installed_bin mcp serve" \
  "claude:mcp remove punakawan --scope user" \
  "claude:mcp add punakawan --scope user -- $installed_bin mcp serve"
do
  grep -F "$expected" "$call_log" >/dev/null || fail "missing client call: $expected"
done

generic_config="$config_dir/mcp-config.json"
grep -F "\"command\": \"$installed_bin\"" "$generic_config" >/dev/null || fail "generic MCP config has wrong command"
grep -F '"args": ["mcp", "serve"]' "$generic_config" >/dev/null || fail "generic MCP config has wrong args"

cat >"$test_root/failing-codex" <<'SCRIPT'
#!/usr/bin/env bash
exit 1
SCRIPT
chmod +x "$test_root/failing-codex"
failure_output="$(
  PUNAKAWAN_CODEX_BIN="$test_root/failing-codex" \
  PUNAKAWAN_CLAUDE_BIN="$test_root/not-installed" \
    bash "$SCRIPT_DIR/configure-agent.sh" "$installed_bin" "$config_dir" 2>&1
)"
assert_contains "$failure_output" "codex mcp add punakawan -- $installed_bin mcp serve"

bash -n "$SCRIPT_DIR/install.sh"

if [[ "$(uname -s)" == "Darwin" ]]; then
  mac_output="$(bash "$SCRIPT_DIR/install.sh" --dry-run)"
  assert_contains "$mac_output" "pnpm --filter @punakawan/panel build"
  assert_contains "$mac_output" "go install ./cmd/punakawan ./cmd/punakawand"
  assert_contains "$mac_output" "punakawan panel"

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
  assert_contains "$windows_output" "pnpm --filter @punakawan/panel build"
  assert_contains "$windows_output" "go install ./cmd/punakawan ./cmd/punakawand"
  assert_contains "$windows_output" "mcp add punakawan"
  assert_contains "$windows_output" "Generic MCP config"
  assert_contains "$windows_output" "punakawan panel"
fi

printf 'installer checks passed\n'
