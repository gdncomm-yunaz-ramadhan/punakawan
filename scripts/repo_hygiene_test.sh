#!/usr/bin/env bash
set -euo pipefail

paths=(
  .github/workflows/ci.yml
  packages/adapter-host
  packages/atlassian-adapter
  packages/gitlab-adapter
  packages/openapi-adapter
  packages/playwright-adapter
  packages/playwright-generator
  packages/playwright-recorder
)

for path in "${paths[@]}"; do
  if [[ -e "$path" ]]; then
    echo "unsupported repository path remains: $path" >&2
    exit 1
  fi
done
