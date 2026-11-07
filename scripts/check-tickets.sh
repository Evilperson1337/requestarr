#!/usr/bin/env bash
# A ticket's Status line must agree with its acceptance criteria (docs/agents/issue-tracker.md):
# `done` means every box is ticked, and `ready-for-agent` means not started, so a ticket whose
# boxes are all ticked cannot still say it. Older tickets say `resolved`; they are left alone.
set -euo pipefail
cd "$(dirname "$0")/.."
status=0
shopt -s nullglob
for file in .scratch/*/issues/*.md; do
  state=$(sed -n 's/^\*\*Status:\*\* *//p; s/^Status: *//p' "$file" | head -n1)
  open=$(grep -c '^- \[ \]' "$file" || true)
  ticked=$(grep -c '^- \[[xX]\]' "$file" || true)
  if [[ "$state" == "done" && "$open" -gt 0 ]]; then
    echo "FAIL: $file: Status is done but $open criteria are unticked (use needs-review)" >&2; status=1
  fi
  if [[ "$state" == "ready-for-agent" && "$open" -eq 0 && "$ticked" -gt 0 ]]; then
    echo "FAIL: $file: every criterion is ticked but Status is still ready-for-agent" >&2; status=1
  fi
done
[[ $status == 0 ]] && echo "ok: ticket status lines match their criteria"
exit $status
