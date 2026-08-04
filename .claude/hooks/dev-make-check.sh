#!/bin/bash
# SubagentStop gate for the dev agent: block completion until /usr/bin/make check passes.
# Exit 0 = allow stop; exit 2 = block, stderr is fed back to the agent.

input=$(cat)

get() {
  printf '%s' "$input" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('$1',''))" 2>/dev/null
}

# Loop guard: if this stop was already blocked once by a hook, let it through.
[ "$(get stop_hook_active)" = "True" ] && exit 0

cwd=$(get cwd)
cd "${cwd:-.}" 2>/dev/null || exit 0

# Only gate inside the Go repo (main checkout or a worktree).
[ -f Makefile ] && [ -f go.mod ] || exit 0

# Only gate work on a feature branch — a dev agent that stopped on main did no code work.
branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null)
case "$branch" in
  main|HEAD|"") exit 0 ;;
esac

out=$(/usr/bin/make check 2>&1)
if [ $? -ne 0 ]; then
  {
    echo "make check failed — you must fix this before finishing (do not open/update the PR until it passes):"
    echo "$out" | tail -40
  } >&2
  exit 2
fi
exit 0
