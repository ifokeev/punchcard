---
description: Run the punchcard Engineer loop — claim tasks and ship them as reviewed PRs with proof of work until the board is clear
---

Run the punchcard **Engineer** loop. Use the `punchcard-engineer` skill for the full
contract (subagent dispatch, inline review, proof capture, memory, failure handling).

Loop until the queue is drained:

1. `punch next --batch` → claims up to the server's **concurrency** limit and prints a
   JSON **array** of tasks (each: `id`, `title`, `description`, `acceptance`, `repo`).
   - Exit code 3 (empty array) → the board is clear → stop cleanly.
   - Exit code 4 (paused) → the board is **paused** → do NOT stop: wait briefly and
     re-check (resume is controlled from the board, you may have no terminal access).
   - **Only exit 3 stops the loop.** Any other nonzero exit is transient → wait briefly
     and retry.
2. Dispatch **one fresh subagent per task in the batch, in parallel** (issue them
   together), each doing the whole task in its own clean context: **create an isolated
   `git worktree` off `origin/<default>`** (never the shared main tree — that sweeps in
   unrelated changes and collides with other agents) → implement → commit (**Conventional
   Commits**) → push → `gh pr create` (conventional PR title) → inline self-review via
   `gh pr diff` (subagents can't call slash commands) → fix → **confirm the PR is
   mergeable** (`gh pr view --json mergeable`; CI checks pass) → capture proof and
   `punch attach <id> <file>` → remove the worktree → return a one-line summary.
   - **Wait for the WHOLE batch to finish before claiming the next one** — that is what
     keeps no more than *concurrency* agents running at once.
   - Each subagent re-checks `punch get <id>` at every checkpoint; if its task is no
     longer `in_progress` (cancelled from the board, or swept) it **aborts cleanly**,
     removes its worktree, and returns `cancelled`.
3. Record state for **each** task: success → `punch update <id> --pr <url> --branch <name>
   --status done`; failure → `punch update <id> --status failed --note "<why>"` (or
   `blocked`); a subagent that returned `cancelled` is already `cancelled` — leave it.
   **A failed/blocked/cancelled task must NOT stop the loop — continue to the next batch.**
4. Repeat.

Keep the loop session thin: it only claims batches and records one-line results; the heavy
work lives and dies in each per-task subagent (that is what keeps context fresh per task).
