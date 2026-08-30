#!/usr/bin/env bash
# Fails when an internal package exists on disk but is not reachable, through
# a non-test import, from any command root. This is the permanent guard
# against the dead architecture this repository has repeatedly accumulated:
# every internal package must either be wired into a real binary or be named
# below with the specific, verified reason it is exempt.
set -euo pipefail

module="github.com/ygrip/punakawan"

# Each entry names an internal package with no non-test importer reachable
# from ./cmd/... and states exactly why it is not dead code to delete.
allowlist=(
  "archcheck:test-only architecture gate; its only invocation is go test ./... (legacy_test.go has no non-test file to import)"
  "contradiction:has no production importer, but internal/scenario's own test still imports it to exercise the contradiction-detection step of that end-to-end scenario"
  "convention:has no production importer, but internal/roleconfig's own test still imports it (convention.RecordNoTernaryConvention) to exercise that resolver path"
  "planimport:one-time legacy plan importer with no caller wired yet; not part of this change, left for a follow-up decision"
  "recipe:has no production importer once internal/workcontext (its only caller) is removed; not part of this change, left for a follow-up decision"
  "scenario:test-only end-to-end scenario package; its only invocation is go test ./... (no non-test file imports it)"
)

is_allowed() {
  local pkg="$1"
  local entry name
  for entry in "${allowlist[@]}"; do
    name="${entry%%:*}"
    if [[ "$pkg" == "${module}/internal/${name}" ]]; then
      return 0
    fi
  done
  return 1
}

reachable=$(go list -deps -test=false ./cmd/... | grep "^${module}/internal/" || true)
all_internal=$(go list ./internal/... | sort -u)

unreferenced=()
while IFS= read -r pkg; do
  [[ -z "$pkg" ]] && continue
  if grep -qxF "$pkg" <<<"$reachable"; then
    continue
  fi
  if is_allowed "$pkg"; then
    continue
  fi
  unreferenced+=("$pkg")
done <<<"$all_internal"

if ((${#unreferenced[@]} > 0)); then
  echo "unreferenced production packages (no non-test import reachable from ./cmd/..., and not in the allowlist):" >&2
  for pkg in "${unreferenced[@]}"; do
    echo "  $pkg" >&2
  done
  exit 1
fi

echo "production import graph is clean"
