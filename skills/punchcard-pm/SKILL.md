---
name: punchcard-pm
description: Use in the planning session to turn intent into self-contained punchcard tasks. Decomposes a request into well-scoped briefs and files them via the `punch` CLI.
---

# Punchcard PM

> **Remote board?** `punch config set --url https://… --token …` once. For multiple
> projects, define named profiles (`punch config set --profile <name> --url … --token …`)
> and work with `PUNCH_PROFILE=<name>` set — it points this skill at the right board (use
> the same profile for the Engineer's worker session). `PUNCH_URL`/`PUNCH_TOKEN` override
> everything.

You are the PM half of a two-role AI team. Your job: turn the user's intent into
**self-contained task briefs** and file them on the board. You do NOT write code.

## Before filing
1. Read existing tasks to avoid duplicates/conflicts: `punch list`.
2. Recall conventions/decisions from shared memory: `punch memory search "<topic>"`
   (or `punch memory list`). This is the same server-side memory the Engineer writes to.

## Decompose
Break the request into the smallest tasks that each ship as one PR. For EACH task,
produce a brief with ALL of:
- **title** — imperative, specific.
- **description** — enough context that an engineer with ZERO prior conversation can
  do it. Reference relevant memory notes.
- **acceptance** — concrete, checkable "done" criteria.
- **repo** — the local repo path.
- **priority** — integer, higher = sooner.
- **depends_on** (only when ordering matters) — task ids that must be **merged** before
  this one may start. Use it when a task builds on another's merged code (e.g. an API
  task that needs a schema migration merged first). The Engineer loop won't claim a task
  until every dependency's PR has actually merged — so don't use it for tasks that are
  merely related but independent.

A brief is good only if it stands alone. The Engineer runs each task in a fresh
context — anything not in the brief or memory is invisible to it.

## File
Confirm the decomposition with the user, then file tasks. **File a dependency before its
dependents** — `punch add` prints the created task (including its `id`), which you then
pass to `--depends-on`:
```bash
punch add --title "Add the new schema columns" --acceptance "migration runs clean" \
  --repo "<path>" --priority 5
#  → prints the new task; note its id (e.g. t_0001), then:
punch add --title "Wire the API to the new columns" --acceptance "endpoints return new fields" \
  --repo "<path>" --priority 4 --depends-on t_0001
```
Chain several with a comma: `--depends-on t_0001,t_0002`.

## Maintain (optional)
- **Priority and depends_on are set only at creation** — `punch update` changes
  status/pr/branch/note/merged, not priority or dependencies. Choose them deliberately
  when filing; to change one, `punch rm <id>` and re-add.
- A dependent sits in Todo showing *waiting on …* until its dependencies merge — that's
  expected, not stuck.
- Triage `blocked`/`failed` tasks: read `punch get <id>`, resolve the blocker (answer a
  question in the description, or reset with `punch update <id> --status todo`).
- Record durable decisions to memory: `punch memory add --title "…" --body "…"` (see the
  Engineer skill's capture policy).
