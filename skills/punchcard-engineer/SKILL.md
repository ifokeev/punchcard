---
name: punchcard-engineer
description: Use in the worker session (driven by /loop) to execute punchcard tasks. Claims the next task, dispatches a fresh subagent to ship it as a reviewed PR with proof of work, and records the result.
---

# Punchcard Engineer

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
   does steps 3–8 and returns ONE line: `id | branch | pr_url | proof_url | outcome`.

3. **(subagent) Load context:** `cd <repo>`; recall relevant notes with
   `punch memory search "<topic/keywords>" --repo <repo>` and read the returned notes.
   Create a branch `punch/<id>-<slug>`.

4. **(subagent) Implement** to satisfy `acceptance`. Run the repo's tests. Commit.

5. **(subagent) Open the PR:** push, then `gh pr create --fill`. Capture the PR URL.
   If `gh pr create` fails, STOP and return outcome=`failed: <reason>`.

6. **(subagent) Self-review INLINE** (subagents cannot call slash commands):
   `gh pr diff` → read the diff critically for bugs/regressions/missed acceptance →
   fix → commit → push. Do NOT invoke `/review` or `/code-review`.

7. **(subagent) Proof of work:** capture a short artifact of the result (Chrome-MCP
   GIF for web changes; a screenshot otherwise) and upload it:
   `punch attach <id> <file>`. Capture the returned proof URL.

8. **(subagent) Return** the one-line summary. If anything in 3–7 failed, return
   outcome=`failed: <reason>` (do not throw away the partial PR URL if one exists).

9. **(loop) Record state** from the subagent's summary:
   - success → `punch update <id> --pr <pr_url> --branch <branch> --status done`
   - failure → `punch update <id> --status failed --note "<reason>"` (or `blocked` if
     it needs a human)
   - **Always continue to the next task.** A single failed task must NOT stop the
     loop — only an empty queue does.

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
