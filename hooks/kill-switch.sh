#!/usr/bin/env bash
# punchcard PreToolUse kill-switch — OPT-IN and FAIL-OPEN.
#
# Cooperative cancel only stops a subagent at its next checkpoint. This hook makes
# the board's "Stop all" an INSTANT halt: when control.stopped is true it denies
# the next tool call, so a running loop/subagent stops within one tool call.
#
# OPT-IN: it does nothing unless PUNCH_KILLSWITCH is set, so it never touches your
# other Claude Code sessions. Start the worker loop with it:
#     PUNCH_KILLSWITCH=1 claude        # then: /loop /punch-loop
#
# FAIL-OPEN: any error (server down, slow, auth) -> allow the tool (exit 0). A dead
# punch server must never freeze your work. The curl is capped at 1s.
#
# Clear the stop from the board (Resume) or `punch resume`.
set -u
[ -n "${PUNCH_KILLSWITCH:-}" ] || exit 0

url="${PUNCH_URL:-http://127.0.0.1:8080}"
url="${url%/}/api/control"

# Two branches (not an array) so this stays portable to macOS bash 3.2, where an
# empty array under `set -u` errors out.
if [ -n "${PUNCH_TOKEN:-}" ]; then
  resp="$(curl -fsS --max-time 1 -H "Authorization: Bearer ${PUNCH_TOKEN}" "$url" 2>/dev/null)" || exit 0
else
  resp="$(curl -fsS --max-time 1 "$url" 2>/dev/null)" || exit 0
fi

case "$resp" in
  *'"stopped":true'*)
    # continue:false halts the turn; permissionDecision:deny blocks the tool —
    # belt and suspenders so a running agent stops immediately either way.
    printf '%s\n' '{"continue":false,"stopReason":"punchcard: Stop all engaged — halting. Clear it with Resume on the board.","hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"punchcard: Stop all engaged"}}'
    ;;
esac
exit 0
