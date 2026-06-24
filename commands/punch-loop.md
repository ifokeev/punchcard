---
description: Run the punchcard Engineer loop — claim tasks and ship them as reviewed PRs with proof of work until the board is clear
---

Run the punchcard **Engineer** loop. Use the `punchcard-engineer` skill for the full
contract (subagent dispatch, inline review, proof capture, memory, failure handling).

Loop until the queue is drained:

1. **Unblock dependents (reconcile merges).** Tasks may declare `depends_on` and the
   server won't hand them out until those dependencies have **merged**. So first run
   `punch list` and, for any task that is `done` with a `pr_url` *and* appears in some
   todo task's `depends_on`, check whether its PR landed:
   `gh pr view <pr_url> --json state -q .state` → if `MERGED`, run
   `punch update <dep_id> --merged`. This is what lets a blocked task become claimable.
   (Only check deps that are actually blocking something — keep it cheap.)
2. `punch next --batch` → claims up to the server's **concurrency** limit and prints a
   JSON **array** of tasks (each: `id`, `title`, `description`, `acceptance`, `repo`).
   Blocked tasks (unmerged dependencies) are simply not returned.
   - Exit code 3 (empty array) → the board is clear → stop cleanly.
   - Exit code 4 (paused) → the board is **paused** → do NOT stop: wait briefly and
     re-check (resume is controlled from the board, you may have no terminal access).
   - **Only exit 3 stops the loop.** Any other nonzero exit is transient → wait briefly
     and retry.
3. Dispatch **one fresh subagent per task in the batch, in parallel** (issue them
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
4. Record state for **each** task: success → `punch update <id> --pr <url> --branch <name>
   --status done`; failure → `punch update <id> --status failed --note "<why>"` (or
   `blocked`); a subagent that returned `cancelled` is already `cancelled` — leave it.
   **A failed/blocked/cancelled task must NOT stop the loop — continue to the next batch.**
   (Marking a dependency `done` doesn't unblock its dependents — that happens in step 1
   once its PR actually merges.)
5. Repeat.

Keep the loop session thin: it only claims batches and records one-line results; the heavy
work lives and dies in each per-task subagent (that is what keeps context fresh per task).
