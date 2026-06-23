#!/usr/bin/env bash
# Pre-stage tasks for a deterministic demo recording.
# NOTE: the seed input is deterministic, but the Engineer executes each task with a
# live LLM that opens REAL `gh pr create` PRs. Point this at a THROWAWAY sandbox repo
# (or `gh` configured against a test remote) — PRs are expected artifacts of the demo.
# Requires: `punch` on PATH; PUNCH_URL/PUNCH_TOKEN exported for non-local servers.
set -euo pipefail
: "${PUNCH_URL:=http://127.0.0.1:8080}"
REPO="${1:?usage: demo-seed.sh <sandbox-repo-path>}"
git -C "$REPO" rev-parse --is-inside-work-tree >/dev/null 2>&1 || { echo "error: $REPO is not a git repo" >&2; exit 1; }
git -C "$REPO" remote get-url origin >/dev/null 2>&1 || { echo "error: $REPO has no 'origin' remote for gh PRs" >&2; exit 1; }
punch add --title "Add a /health endpoint" --repo "$REPO" --priority 3 \
  --description "Add GET /health returning 200 ok." \
  --acceptance "curl /health returns 200 and body 'ok'."
punch add --title "Add CONTRIBUTING.md" --repo "$REPO" --priority 2 \
  --description "Write a short CONTRIBUTING.md." \
  --acceptance "CONTRIBUTING.md exists with build+test steps."
punch add --title "Fix typo in README title" --repo "$REPO" --priority 1 \
  --description "Correct the misspelled project title." \
  --acceptance "README h1 spelled correctly."
echo "seeded 3 tasks against $REPO"
