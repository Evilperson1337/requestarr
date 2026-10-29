#!/usr/bin/env bash
# Claude Code PreToolUse hook for Bash. Denies commands that can lose work the
# repository does not hold: deleting untracked files, and unstaging deletions
# with git rm --cached. Reads the hook JSON on stdin.
set -euo pipefail
command="$(jq -r '.tool_input.command // ""')"
deny() {
	jq -n --arg reason "$1" '{hookSpecificOutput: {hookEventName: "PreToolUse", permissionDecision: "deny", permissionDecisionReason: $reason}}'
	exit 0
}
# Only a git rm that starts a command (line start or after ; & |), so text that
# merely mentions it, such as a commit message, passes.
if grep -qE '(^|[;&|])[[:space:]]*git[[:space:]]+rm[[:space:]][^;&|]*--cached' <<<"$command"; then
	deny "git rm --cached changes the index outside a commit step; stage with git add/git restore --staged instead."
fi
cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
# Every word after an rm is a candidate path; flag ones git does not track.
while read -r target; do
	[[ -z "$target" || "$target" == -* ]] && continue
	if [[ -e "$target" ]] && git ls-files --error-unmatch -- "$target" >/dev/null 2>&1; then
		continue
	fi
	if [[ -e "$target" ]] && ! git check-ignore -q -- "$target"; then
		deny "rm would delete untracked file $target, which git cannot restore. Ask the user first, or move it aside."
	fi
done < <(grep -oE '(^|[;&|[:space:]])rm([[:space:]]+[^;&|]+)' <<<"$command" | sed -E 's/^.?rm[[:space:]]+//' | tr ' ' '\n')
exit 0
