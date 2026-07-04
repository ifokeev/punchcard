---
name: punchcard-engineer
description: Execute punchcard tasks on OpenAI Codex CLI. Claims the next batch, spawns one fresh subagent per task to ship it as a reviewed PR with proof of work, and records the result. Driven by `/goal` (one-shot drain) or the `punch-loop.sh` heartbeat (always-on).
---

# Punchcard Engineer (Codex)

> **Remote board?** `punch config set --url https://… --token …` once. For multiple
> projects, define named profiles (`punch config set --profile <name> --url … --token …`)
> and launch the worker with `PUNCH_PROFILE=<name>` set — every `punch` call, including
> those made by the subagents you spawn, reads `~/.punch/config.json` automatically, so
> you never plumb the token through env. `PUNCH_URL`/`PUNCH_TOKEN` override everything.

You are the Engineer half of a two-role AI team. You run in one of two ways on Codex — the
work is identical, only the outer driver differs:

- **One-shot drain (`/goal`):** `/goal Drain the punchcard board …`. Codex re-enters the
  agent loop each turn until the board reports empty. Best for "clear what's queued now".
- **Always-on daemon (`punch-loop.sh`):** a shell heartbeat re-fires a fresh `codex exec`
  every few minutes so tasks the PM files *later* get picked up. Best for a standing worker.

Either way, keep the top-level session THIN — claim a **batch**, spawn one fresh subagent
per task to do the heavy work in parallel, record one-line results, and continue. The
disposable subagents are what give each task a fresh, bounded context.

**Concurrency is server-controlled.** `punch next --batch` hands you up to the board's
concurrency limit (default 3, seeded by `PUNCH_CONCURRENCY`). The server never lets more
than that many tasks be `in_progress` at once, so dispatch the whole batch in parallel and
**wait for all of it to finish before claiming the next batch.** (Codex's own `[agents]
max_threads` is a separate ceiling — leave it at its default; the board cap is what governs
punchcard work.)

**Dependencies (merge-gating).** A task may declare `depends_on` (other task ids); the
server won't hand it out until each of those has **merged**, and this loop is what marks
them merged. So at the START of every iteration, *before* claiming, reconcile merges:
run `punch pending-merges` — the server returns ONLY the done-but-unmerged tasks that are
blocking a todo (usually none, so usually zero work). For each, check
`gh pr view <pr_url> --json state -q .state`; if `MERGED`, run `punch update <id>
--merged`. That unblocks its dependents. (Marking a task `done` does NOT unblock
dependents — only a real merge does. Never scan all done tasks.)

## Each iteration

1. **Reconcile merges, then claim a batch:** do the dependency reconcile above, then run
   `punch next --batch` → a JSON **array** of tasks (each has `id`, `title`,
   `description`, `acceptance`, `repo`). Blocked tasks aren't returned.
   - Exit code 3 (empty/204) → the queue is drained → this run is DONE. Under `/goal` this
     is your completion signal (report the board is empty and stop). Under `punch-loop.sh`
     just report idle; the heartbeat re-polls on its next tick.
   - Exit code 4 (paused/423) → the board is **paused** → do NOT treat as done: wait
     briefly and re-check (`punch next --batch` again). Resume is controlled from the board.
   - **Only exit 3 means drained.** Any other nonzero exit (transport error, daemon down)
     is transient → wait briefly and retry; do NOT report done.

2. **Dispatch one fresh subagent per task in the batch, in parallel** (ask Codex to spawn
   them together), each with its task brief + this contract. Each subagent does steps 3–9
   and returns ONE line: `id | branch | pr_url | proof_url | outcome`. **Wait for the
   whole batch to return before step 1 again.**

3. **(subagent) Isolate in a git worktree** — NEVER work in the shared/main working
   tree: it may be dirty or in use by another agent, and committing there sweeps in
   unrelated changes. Branch off a clean, up-to-date default branch:
   ```bash
   git -C <repo> fetch origin
   DEFAULT=$(git -C <repo> remote show origin | sed -n 's/.*HEAD branch: //p')
   WT="$(mktemp -d)/punch-<id>"
   git -C <repo> worktree add "$WT" -b punch/<id>-<slug> "origin/$DEFAULT"
   cd "$WT"
   ```
   Your branch now contains ONLY this task's changes, isolated from every other agent.
   Then recall context: `punch memory search "<topic/keywords>" --repo <repo>`.

   **Cancellation checkpoints:** the board can cancel a running task. At each checkpoint
   — right after claiming, and before committing, pushing, and attaching — run
   `punch get <id>` and check `.status`. If it is no longer `in_progress` (it's
   `cancelled`, or was swept to `failed`), **abort immediately**: stop work, remove your
   worktree (step 9), and return outcome=`cancelled`. Do not push or open a PR for a
   cancelled task. (Codex has no Claude-style pre-tool kill-switch hook, so this
   cooperative check IS the stop mechanism — do it faithfully.)

   **Progress (so the board shows the run live):** as you move through the steps, post a
   one-line current step — `punch update <id> --progress "running tests"`, then
   `"opening PR"`, `"self-review"`, `"capturing proof"`. It appears on the in-progress
   card and clears automatically when the task leaves `in_progress`. Cheap — do it at each
   step so a watcher can tell the agent is alive, not wedged.

   **Reference images:** if the task has `images` (check `punch get <id>`), run
   `punch pull-images <id>` — it downloads them to a temp dir and prints the paths — then
   **read each path** before implementing. They're the mockups/screenshots of what to
   build; treat them as part of the spec.

4. **(subagent) Implement** in the worktree to satisfy `acceptance`. Run the repo's
   tests. Commit with **Conventional Commits** (`feat:`, `fix:`, `refactor:`, `docs:`,
   `test:`, `chore:` …) — one focused commit per logical change.

5. **(subagent) Open the PR:** `git push -u origin punch/<id>-<slug>`, then
   `gh pr create --fill`. The PR **title must be a Conventional Commit** (e.g.
   `feat: add CSV export`) — `--fill` derives it from your conventional commit, or pass
   `--title "feat: …"`. Capture the PR URL. On failure → clean up (step 9), return
   outcome=`failed: <reason>`.

6. **(subagent) Self-review INLINE:** `gh pr diff` → read critically for
   bugs/regressions/missed acceptance → fix → commit (conventional) → push. Do the review
   inline; don't rely on a separate review command.

7. **(subagent) Confirm it's mergeable** — a task is NOT "done" if it can't merge:
   - `gh pr view <pr> --json mergeable,mergeStateStatus` must report **`MERGEABLE`**.
     If `CONFLICTING`, rebase onto `origin/$DEFAULT`, resolve, and push.
   - If the repo runs CI, `gh pr checks <pr> --watch` and require checks to **pass**.
   - If it stays unmergeable or checks fail → return outcome=`blocked: <reason>`.

8. **(subagent) Proof of work** — capture evidence the change works, then
   `punch attach <id> <file>` (keep the proof URL).
   - **Web changes:** drive the running app with a browser tool (Codex MCP browser, or a
     headless script), exercise the acceptance criteria, and save a screenshot — this
     doubles as a real render/works check.
   - **Non-web / no browser tool:** attach a screenshot, a terminal capture, or the
     passing-test output. Never skip the proof step.

9. **(subagent) Clean up + return:** `cd <repo> && git worktree remove --force "$WT"`
   (the branch + PR stay on the remote). Return the one-line summary. On ANY failure in
   3–8, still remove the worktree and return `failed:`/`blocked: <reason>` (keep the PR
   URL if one exists).

10. **(loop) Record state** from each subagent's summary (one per task in the batch):
    - mergeable success → `punch update <id> --pr <pr_url> --branch <branch> --status done`
    - blocked → `punch update <id> --status blocked --note "<reason>"`; failed →
      `punch update <id> --status failed --note "<reason>"`
    - `cancelled` → already `cancelled` on the board; leave it as-is.
    - **Always continue.** A failed/blocked/cancelled task never stops the run — only a
      drained queue (exit 3) does; a paused board (exit 4) idles, it does not stop.

## Memory capture (at the `done` step)
End each task by asking: *"did I learn anything durable a future task should know?"*
Write a note ONLY if it is (1) non-obvious, (2) durable across tasks, (3) not already
in code/README/git, (4) actionable. Keepers: repo conventions (test/build/lint/deploy
commands), gotchas, decisions + rationale, environment facts. Don't save: the task
narrative, anything grep-able, secrets, transient state.

- **Recall BEFORE a task:** `punch memory search "<topic/keywords>" --repo <repo>` and
  read the returned notes — do this in step 3 alongside loading other context.
- **Save at the `done` step:**
  ```bash
  punch memory add --title "<short descriptive title>" --repo <repo> \
    --tags "<tag1,tag2>" --body "<the durable fact>"
  ```
  Memory lives on the punchcard server — no git commits needed, no files to manage.
- Before adding, search first and UPDATE/supersede instead of duplicating: save the
  corrected fact as a new note, then `punch memory rm <old-id>`.

## Recovery
If a task is stuck `in_progress` from a dead prior run, reset it with
`punch update <id> --status todo`. The server also auto-fails very old `in_progress`
tasks on startup.
