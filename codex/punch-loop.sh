#!/usr/bin/env bash
# punch-loop.sh — always-on punchcard worker for OpenAI Codex CLI.
#
# The Codex equivalent of Claude Code's `/loop 5m /punch-loop`: re-fires a FRESH
# `codex exec` drain every INTERVAL seconds, so tasks the PM files later get picked
# up. Each tick runs the punchcard-engineer skill for one batch and exits; this
# outer loop is what keeps the worker standing.
#
# For a one-shot "clear the queue now, then stop" run, use `/goal` instead — see
# codex/README.md. `/goal` self-corrects across turns and stops when the board is
# empty; this script is for a worker that must survive an empty queue and resume.
#
# Prereqs (once): `punch config set --url … --token …` so every `punch` call —
# including the ones the spawned subagents make — reads ~/.punch/config.json.
# Point at a specific board with PUNCH_PROFILE.
#
# Usage:  ./punch-loop.sh
#   PUNCH_LOOP_INTERVAL=120 ./punch-loop.sh      # poll every 2 min (default 300)
#   PUNCH_PROFILE=side       ./punch-loop.sh      # drive a named board
set -uo pipefail

INTERVAL="${PUNCH_LOOP_INTERVAL:-300}"

read -r -d '' PROMPT <<'EOF' || true
Use the punchcard-engineer skill. Reconcile merges (punch pending-merges), then run
`punch next --batch` to claim a batch. Spawn one fresh subagent per task, in parallel,
to ship each as a reviewed PR with proof of work, and record every result. If the queue
is drained (punch next --batch exits 3), report the board is idle and end this run — the
outer loop will re-poll. If the board is paused (exit 4), wait briefly and retry. A single
failed or blocked task must NOT stop the run; continue to the next batch.
EOF

echo "punch-loop: draining the board every ${INTERVAL}s (Ctrl-C to stop)"
trap 'echo; echo "punch-loop: stopped."; exit 0' INT

while true; do
  # workspace-write lets the agent edit/commit; subagents create worktrees under
  # $TMPDIR. On a locked-down VM you may need --sandbox danger-full-access instead.
  codex exec --skip-git-repo-check --sandbox workspace-write "$PROMPT" \
    || echo "punch-loop: codex exec exited $? — treating as transient, retrying next tick"
  sleep "$INTERVAL"
done
