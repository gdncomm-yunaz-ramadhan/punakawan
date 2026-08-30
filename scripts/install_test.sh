#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

assert_contains() {
  local output="$1"
  local expected="$2"
  [[ "$output" == *"$expected"* ]] || fail "output missing: $expected"
}

# run_relocation_test installs Punakawan for real into an isolated,
# throwaway data/install prefix from a disposable copy of this checkout,
# deletes that copy entirely, and only then verifies doctor, both
# adapters' initialize handshakes, a lifecycle hook probe reaching the
# relocated telemetry spool/database, and credential redaction - all
# against an install that has outlived the source checkout it was built
# from. Macos-only, like install.sh itself; set
# PUNAKAWAN_SKIP_RELOCATION_TEST=1 to skip it (it is the slowest part of
# this suite: a real build plus a real `pnpm install`).
run_relocation_test() {
  printf '\n==> Real install into a relocated prefix, then the source checkout is deleted\n'
  local reloc_root checkout_copy data_dir install_dir fake_home outside_dir
  reloc_root="$(mktemp -d "${TMPDIR:-/tmp}/punakawan-relocation.XXXXXX")"
  checkout_copy="$reloc_root/checkout"
  data_dir="$reloc_root/data"
  install_dir="$reloc_root/bin"
  fake_home="$reloc_root/home"
  outside_dir="$reloc_root/outside"
  mkdir -p "$fake_home" "$outside_dir"

  # A plain recursive copy of the actual working tree - not a `git
  # worktree` or a fresh clone - so this test verifies the checkout as it
  # stands right now (including uncommitted changes), not just the last
  # commit. node_modules/.git/store directories are excluded: pnpm relinks
  # dependencies from its own machine-wide content-addressable store, so
  # nothing under them is needed for a real install to succeed here.
  rsync -a \
    --exclude='.git' --exclude='node_modules' --exclude='.pnpm-store' \
    --exclude='.worktrees' --exclude='.claude' --exclude='.serena' --exclude='bin' \
    "$REPO_ROOT/" "$checkout_copy/"

  # PUNAKAWAN_SKIP_HOOKS=1 keeps this step from touching this machine's
  # real ~/.codex or ~/.claude - hook installation against an isolated
  # $HOME is verified explicitly, separately, below. It deliberately does
  # NOT also override HOME for this call: Go's module cache and pnpm's
  # store both key off the real $HOME, and pointing them at a fresh one
  # would force a full, slow re-populate instead of reusing what this
  # machine already has cached.
  (
    cd "$checkout_copy"
    PUNAKAWAN_DATA_DIR="$data_dir" \
    PUNAKAWAN_INSTALL_DIR="$install_dir" \
    PUNAKAWAN_CODEX_BIN="/nonexistent-codex-binary" \
    PUNAKAWAN_CLAUDE_BIN="/nonexistent-claude-binary" \
    PUNAKAWAN_SKIP_HOOKS="1" \
      bash scripts/install.sh
  )

  rm -rf "$checkout_copy"

  grep -F "$data_dir/adapters/atlassian" "$data_dir/config.yaml" >/dev/null \
    || fail "relocation: global config does not point the atlassian adapter below the install data directory"
  grep -F "$data_dir/adapters/github" "$data_dir/config.yaml" >/dev/null \
    || fail "relocation: global config does not point the github adapter below the install data directory"
  if grep -F "$checkout_copy" "$data_dir/config.yaml" >/dev/null; then
    fail "relocation: global config still references the deleted source checkout"
  fi

  printf '\n==> doctor --json, and both adapters'"'"' initialize handshakes, against the relocated install\n'
  local doctor_output
  doctor_output="$(cd "$outside_dir" && HOME="$fake_home" PUNAKAWAN_DATA_DIR="$data_dir" "$install_dir/punakawan" doctor --json || true)"
  if command -v python3 >/dev/null 2>&1; then
    python3 - "$doctor_output" <<'PY'
import json, sys
report = json.loads(sys.argv[1])
for adapter in ("atlassian", "github"):
    status = report["adapters"][adapter]
    assert status["entrypoint"] == "ok", f"relocation: {adapter} entrypoint = {status['entrypoint']!r}"
    assert status["handshake"] == "ok", f"relocation: {adapter} handshake = {status['handshake']!r}"
PY
  else
    assert_contains "$doctor_output" '"entrypoint": "ok"'
    assert_contains "$doctor_output" '"handshake": "ok"'
  fi

  printf '\n==> lifecycle hook install and probe against the relocated install\n'
  HOME="$fake_home" PUNAKAWAN_DATA_DIR="$data_dir" "$install_dir/punakawan" setup --hooks-only >/dev/null

  local hook_doctor_output
  hook_doctor_output="$(cd "$outside_dir" && HOME="$fake_home" PUNAKAWAN_DATA_DIR="$data_dir" "$install_dir/punakawan" doctor --json || true)"
  assert_contains "$hook_doctor_output" '"codex": "complete"'
  if find "$data_dir/telemetry-spool" -type f 2>/dev/null | grep -q .; then
    fail "relocation: a successfully ingested hook probe left a file behind in the telemetry spool"
  fi

  printf '\n==> credential redaction against the relocated install\n'
  local secret="reloc-test-secret-should-never-appear-in-output"
  local redaction_output
  redaction_output="$(cd "$outside_dir" && HOME="$fake_home" PUNAKAWAN_DATA_DIR="$data_dir" \
    ATLASSIAN_HOST="example.atlassian.net" ATLASSIAN_EMAIL="agent@example.com" ATLASSIAN_API_TOKEN="$secret" GITHUB_TOKEN="$secret" \
    "$install_dir/punakawan" doctor --json 2>&1 || true)"
  if [[ "$redaction_output" == *"$secret"* ]]; then
    fail "relocation: doctor leaked a raw credential value"
  fi

  rm -rf "$reloc_root"
  printf 'relocation test passed\n'
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
  dry_run_data_dir="$test_root/dry-run-data"
  mac_output="$(PUNAKAWAN_DATA_DIR="$dry_run_data_dir" bash "$SCRIPT_DIR/install.sh" --dry-run)"
  assert_contains "$mac_output" "pnpm --filter @punakawan/schema-types build"
  assert_contains "$mac_output" "pnpm --filter @punakawan/adapter-sdk build"
  assert_contains "$mac_output" "pnpm --filter @punakawan/adapter-atlassian build"
  assert_contains "$mac_output" "pnpm --filter @punakawan/github-adapter build"
  assert_contains "$mac_output" "pnpm -r --if-present build"
  assert_contains "$mac_output" "go install ./cmd/punakawan ./cmd/punakawand"
  assert_contains "$mac_output" "Configuring global adapters"
  assert_contains "$mac_output" "setup --hooks-only"
  assert_contains "$mac_output" "punakawan panel"
  assert_contains "$mac_output" "$dry_run_data_dir/adapters/atlassian"
  assert_contains "$mac_output" "$dry_run_data_dir/adapters/github"
  [[ "$mac_output" != *"$REPO_ROOT/packages/adapter-atlassian/dist"* ]] || fail "dry run still references the checkout's own packages/adapter-atlassian/dist"
  [[ "$mac_output" != *"$REPO_ROOT/packages/github-adapter/dist"* ]] || fail "dry run still references the checkout's own packages/github-adapter/dist"

  override_output="$(PUNAKAWAN_DATA_DIR="$dry_run_data_dir" PUNAKAWAN_INSTALL_DIR="$test_root/override-bin" bash "$SCRIPT_DIR/install.sh" --dry-run)"
  [[ "$override_output" != *"Added $test_root/override-bin to PATH"* ]] || fail "override install edits shell PATH"
  [[ "$override_output" != *"append"* ]] || fail "override install prints shell PATH update"

  if bash "$SCRIPT_DIR/install.sh" --unknown-option >/dev/null 2>&1; then
    fail "macOS installer accepted an unknown option"
  fi

  if [[ "${PUNAKAWAN_SKIP_RELOCATION_TEST:-0}" != "1" ]]; then
    run_relocation_test
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
