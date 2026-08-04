#!/bin/bash
# PostToolUse(Bash) hook: after a successful `gh pr create`, sync the issue's
# status labels automatically (ready-for-dev -> ready-for-review) so label state
# no longer depends on agent discipline. Best-effort: never blocks, never fails.

input=$(cat)

printf '%s' "$input" | python3 -c '
import json, re, subprocess, sys

try:
    data = json.load(sys.stdin)
except Exception:
    sys.exit(0)

cmd = (data.get("tool_input") or {}).get("command", "")
if "gh pr create" not in cmd:
    sys.exit(0)

cwd = data.get("cwd") or "."
try:
    branch = subprocess.run(
        ["git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD"],
        capture_output=True, text=True, timeout=10,
    ).stdout.strip()
    m = re.match(r"(?:feat|fix|chore|docs|test|refactor)/(\d+)-", branch)
    if not m:
        sys.exit(0)
    issue = m.group(1)
    subprocess.run(
        ["gh", "issue", "edit", issue, "--repo", "vibewarden/vibewarden",
         "--remove-label", "status:ready-for-dev",
         "--add-label", "status:ready-for-review"],
        capture_output=True, timeout=30,
    )
except Exception:
    pass
'
exit 0
