#!/usr/bin/env bash
# PreToolUse hook: route Jira/Confluence through Punakawan, with a fallback.
#
# Punakawan is the intended surface for Jira/Confluence (assessment, workflow,
# approval-gated writes). The raw Atlassian MCP is still connected so genuinely
# uncovered operations remain reachable - but the model tends to reach for it
# first, so this hook DENIES the raw Atlassian tools by default and feeds a
# steering message back, nudging the model to the mcp__punakawan__* equivalent.
#
# Fallback / escape hatch: any tool listed in the sibling allow-file passes
# through untouched. When Punakawan has no equivalent for some Atlassian op,
# add that tool name (or a glob) to the allow-file instead of editing code.
#
# Wiring (.claude/settings.json):
#   "PreToolUse": [{ "matcher": "mcp__claude_ai_Atlassian__.*",
#     "hooks": [{ "type": "command",
#       "command": ".claude/hooks/route-to-punakawan.sh" }] }]
#
# Requires: jq (falls back to python3). If neither is present the hook fails
# open (allows the call) rather than blocking everything.
set -euo pipefail

# Prefix of the MCP server to gate. Change if your Jira MCP has another name
# (run /mcp in Claude Code to see connected server names).
GATED_PREFIX="mcp__claude_ai_Atlassian__"

HOOK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ALLOW_FILE="$HOOK_DIR/atlassian-fallback.allow"

input="$(cat)"

# Extract the tool name without hard-depending on jq.
tool=""
if command -v jq >/dev/null 2>&1; then
  tool="$(printf '%s' "$input" | jq -r '.tool_name // empty')"
elif command -v python3 >/dev/null 2>&1; then
  tool="$(printf '%s' "$input" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("tool_name",""))' 2>/dev/null || true)"
fi

# Not an Atlassian call, or we could not parse: defer to normal permission flow.
if [[ -z "$tool" || "$tool" != "$GATED_PREFIX"* ]]; then
  exit 0
fi

# Fallback allow-list: if this tool matches an allow entry, let it through.
if [[ -f "$ALLOW_FILE" ]]; then
  while IFS= read -r pattern || [[ -n "$pattern" ]]; do
    pattern="${pattern%%#*}"                       # strip inline comments
    pattern="$(printf '%s' "$pattern" | tr -d '[:space:]')"
    [[ -z "$pattern" ]] && continue
    # shellcheck disable=SC2053  # intentional glob match, not literal
    if [[ "$tool" == $pattern ]]; then
      exit 0
    fi
  done < "$ALLOW_FILE"
fi

# Deny with a steering reason the model reads and self-corrects on.
reason="Route Jira/Confluence through mcp__punakawan__* tools (submit_jira_assessment, ingest_jira_requirement, sync_jira_subtasks, update_jira_task_progress, request_jira_clarification, jira_assign_issue/link_issues/set_story_points, or call_adapter_operation for approval-gated writes). The raw Atlassian MCP ($tool) is blocked. If Punakawan genuinely has no equivalent, add $tool to .claude/hooks/atlassian-fallback.allow and retry."

if command -v jq >/dev/null 2>&1; then
  jq -n --arg r "$reason" '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "deny",
      permissionDecisionReason: $r
    }
  }'
else
  # jq absent but python3 present (we parsed the tool above, so one of them is).
  python3 - "$reason" <<'PY'
import json, sys
print(json.dumps({
    "hookSpecificOutput": {
        "hookEventName": "PreToolUse",
        "permissionDecision": "deny",
        "permissionDecisionReason": sys.argv[1],
    }
}))
PY
fi
exit 0
