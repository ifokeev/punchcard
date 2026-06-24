---
name: punchcard-engineer
description: Use in the worker session (driven by /loop) to execute punchcard tasks. Claims the next task, dispatches a fresh subagent to ship it as a reviewed PR with proof of work, and records the result.
---

# Punchcard Engineer

> **Remote board?** Run `punch config set --url https://… --token …` once so this skill
> (and every subagent it dispatches) reaches the right server; `PUNCH_URL`/`PUNCH_TOKEN`
> env vars override the config file when set.

You are the Engineer half of a two-role AI team, driven by `/loop`. The loop session
stays THIN — it claims a **batch** of tasks, dispatches one fresh subagent per task to
do the work in parallel, records one-line results, and continues. Keeping the heavy work
in disposable subagents is what gives each task a fresh, bounded context (the `/loop`
session itself accumulates; the subagents do not).

**Concurrency is server-controlled.** `punch next --batch` hands you up to the board's
concurrency limit (default 3, seeded by `PUNCH_CONCURRENCY`). The server never lets more
than that many tasks be `in_progress` at once — so dispatch the whole batch in parallel
and **wait for all of it to finish before claiming the next batch.**

## Each iteration

1. **Claim a batch:** run `punch next --batch` → a JSON **array** of tasks (each has
   `id`, `title`, `description`, `acceptance`, `repo`).
   - Exit code 3 (empty/204) → the queue is drained → STOP the loop cleanly.
   - Exit code 4 (paused/423) → the board is **paused** → do NOT stop: wait briefly and
     re-check (`punch next --batch` again). Resume is controlled from the board.
   - **ONLY exit 3 stops the loop.** Any other nonzero exit (transport error, daemon
     down) is transient → wait briefly and retry; do NOT stop.

2. **Dispatch one fresh subagent per task in the batch, in parallel** (issue them in a
   single step), each with its task brief + this contract. Each subagent does steps 3–9
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
   cancelled task.

4. **(subagent) Implement** in the worktree to satisfy `acceptance`. Run the repo's
   tests. Commit with **Conventional Commits** (`feat:`, `fix:`, `refactor:`, `docs:`,
   `test:`, `chore:` …) — one focused commit per logical change.

5. **(subagent) Open the PR:** `git push -u origin punch/<id>-<slug>`, then
   `gh pr create --fill`. The PR **title must be a Conventional Commit** (e.g.
   `feat: add CSV export`) — `--fill` derives it from your conventional commit, or pass
   `--title "feat: …"`. Capture the PR URL. On failure → clean up (step 9), return
   outcome=`failed: <reason>`.

6. **(subagent) Self-review INLINE** (subagents cannot call slash commands):
   `gh pr diff` → read critically for bugs/regressions/missed acceptance → fix →
   commit (conventional) → push. Do NOT invoke `/review` or `/code-review`.

7. **(subagent) Confirm it's mergeable** — a task is NOT "done" if it can't merge:
   - `gh pr view <pr> --json mergeable,mergeStateStatus` must report **`MERGEABLE`**.
     If `CONFLICTING`, rebase onto `origin/$DEFAULT`, resolve, and push.
   - If the repo runs CI, `gh pr checks <pr> --watch` and require checks to **pass**.
   - If it stays unmergeable or checks fail → return outcome=`blocked: <reason>`.

8. **(subagent) Proof of work** — capture evidence the change works, then
   `punch attach <id> <file>` (keep the proof URL). **This skill owns only the
   *attach*; the browser "how" is delegated** — do not restate another tool's commands
   here.
   - **Web changes:** drive the running app with the
     [agent-browser](https://github.com/vercel-labs/agent-browser) skill (install
     agent-browser + its skills per its README; follow that skill, or `agent-browser
     --help`, for the actual commands). Open the app, exercise the acceptance criteria in
     the browser, and save a screenshot — this doubles as a real render/works check.
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
    - **Always continue.** A failed/blocked/cancelled task never stops the loop — only a
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
- Before adding, search first (`punch memory search`) and UPDATE/supersede instead of
  duplicating. To supersede a stale note: save the corrected fact as a new note, then
  `punch memory rm <old-id>` (or PATCH `superseded_by` on the old note via
  `punch memory get <old-id>` to find it first).
- If a note proved wrong this task, delete or supersede it.

## Recovery
If a task is stuck `in_progress` from a dead prior run, reset it with
`punch update <id> --status todo`. The server also auto-fails very old `in_progress`
tasks on startup.
