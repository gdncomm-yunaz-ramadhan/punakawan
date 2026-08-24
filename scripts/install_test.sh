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
  assert_contains "$windows_output" "punakawan panel"
fi

printf 'installer checks passed\n'
