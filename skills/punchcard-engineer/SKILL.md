---
name: punchcard-engineer
description: Use in the worker session (driven by /loop) to execute punchcard tasks. Claims the next task, dispatches a fresh subagent to ship it as a reviewed PR with proof of work, and records the result.
---

# Punchcard Engineer

> **Remote board?** Run `punch config set --url https://… --token …` once so this skill
> (and every subagent it dispatches) reaches the right server; `PUNCH_URL`/`PUNCH_TOKEN`
> env vars override the config file when set.

You are the Engineer half of a two-role AI team, driven by `/loop`. The loop session
stays THIN — it claims a task, dispatches a fresh subagent to do the work, records a
one-line result, and continues. Keeping the heavy work in a disposable subagent is
what gives each task a fresh, bounded context (the `/loop` session itself
accumulates; the subagent does not).

## Each iteration

1. **Claim:** run `punch next`.
   - Exit code 3 (empty/204) → the queue is drained → STOP the loop cleanly.
   - **ONLY exit 3 stops the loop.** Any other nonzero exit (transport error, daemon
     down) is transient → wait briefly and retry `punch next`; do NOT stop.
   - On success, parse the returned JSON task (has `id`, `title`, `description`,
     `acceptance`, `repo`).

2. **Dispatch a fresh subagent** with the task brief + this contract. The subagent
   does steps 3–9 and returns ONE line: `id | branch | pr_url | proof_url | outcome`.

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

8. **(subagent) Proof of work:** capture an artifact (Chrome-MCP GIF for web changes;
   a screenshot otherwise) and `punch attach <id> <file>`. Capture the proof URL.

9. **(subagent) Clean up + return:** `cd <repo> && git worktree remove --force "$WT"`
   (the branch + PR stay on the remote). Return the one-line summary. On ANY failure in
   3–8, still remove the worktree and return `failed:`/`blocked: <reason>` (keep the PR
   URL if one exists).

10. **(loop) Record state** from the subagent's summary:
    - mergeable success → `punch update <id> --pr <pr_url> --branch <branch> --status done`
    - blocked → `punch update <id> --status blocked --note "<reason>"`; failed →
      `punch update <id> --status failed --note "<reason>"`
    - **Always continue.** One failed/blocked task never stops the loop — only a
      drained queue does.

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
