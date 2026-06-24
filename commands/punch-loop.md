---
description: Run the punchcard Engineer loop — claim tasks and ship them as reviewed PRs with proof of work until the board is clear
---

Run the punchcard **Engineer** loop. Use the `punchcard-engineer` skill for the full
contract (subagent dispatch, inline review, proof capture, memory, failure handling).

Loop until the queue is drained:

1. `punch next`.
   - Exit code 3 (empty) → the board is clear → stop cleanly.
   - **Only exit 3 stops the loop.** Any other nonzero exit is transient → wait briefly
     and retry `punch next`.
   - Otherwise parse the returned task JSON (`id`, `title`, `description`, `acceptance`,
     `repo`).
2. Dispatch a **fresh subagent** with that brief to do the whole task in its own clean
   context: **create an isolated `git worktree` off `origin/<default>`** (never the
   shared main tree — that sweeps in unrelated changes and collides with other agents)
   → implement → commit (**Conventional Commits**) → push → `gh pr create` (conventional
   PR title) → inline self-review via `gh pr diff` (subagents can't call slash commands)
   → fix → **confirm the PR is mergeable** (`gh pr view --json mergeable`; CI checks pass)
   → capture proof and `punch attach <id> <file>` → remove the worktree → return a
   one-line summary.
3. Record state from the summary: success → `punch update <id> --pr <url> --branch <name>
   --status done`; failure → `punch update <id> --status failed --note "<why>"` (or
   `blocked`). **A failed task must NOT stop the loop — continue to the next.**
4. Repeat.

Keep the loop session thin: it only claims tasks and records one-line results; the heavy
work lives and dies in each per-task subagent (that is what keeps context fresh per task).
