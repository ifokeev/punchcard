# Punchcard on OpenAI Codex CLI

The `punch` binary, the board, and the API are agent-agnostic — the same server that
drives Claude Code drives Codex. Only the plugin layer differs, and Codex has native
equivalents: **skills** (same `SKILL.md` format) and two ways to run the loop.

## 1. Install the skills
Codex reads skills from `~/.codex/skills/` (personal, all projects) or `.codex/skills/`
(committed per-project). Copy both:

```sh
mkdir -p ~/.codex/skills
cp -R codex/skills/punchcard-engineer ~/.codex/skills/
cp -R codex/skills/punchcard-pm       ~/.codex/skills/
```

Codex picks up skill changes automatically (restart if it doesn't). The **PM** skill files
tasks in your planning session; the **Engineer** skill ships them. Same two-role split as
Claude Code.

## 2. Point at the board (once)
```sh
punch config set --url https://your-board.example.com --token <token>
# multiple boards: punch config set --profile side --url … --token … ; then PUNCH_PROFILE=side
```
This writes `~/.punch/config.json` (mode 0600). **Every `punch` call — including the ones
the spawned subagents make — reads it automatically**, so you never plumb the token through
environment variables. (`PUNCH_URL`/`PUNCH_TOKEN` override the file when set.)

## 3. Run the loop — pick a path

### A. One-shot drain — `/goal` (recommended for "clear the queue now")
Codex's `/goal` (the "Ralph Loop", stable since v0.133) re-enters the agent loop each turn
until a completion condition holds — perfect for draining the board once, because punchcard
gives it a *hard* signal (`punch next --batch` exits 3 when empty) instead of an LLM guess:

```
/goal Drain the punchcard board using the punchcard-engineer skill. Each turn: reconcile
merges, run `punch next --batch`, spawn one subagent per task to ship it as a reviewed PR
with proof of work, and record results. Done ONLY when `punch next --batch` exits 3 (empty
queue). If it exits 4 (paused), wait and retry — not done. Never stop on a single failed task.
```

`/goal` self-corrects across turns, survives Ctrl-C (state resumes), and soft-stops on its
token budget. Manage it with `/goal pause`, `/goal resume`, `/goal clear`.

### B. Always-on daemon — `punch-loop.sh` (for a standing worker)
`/goal` stops when the queue drains, so it won't pick up work filed *later*. For a worker
that must keep polling, use the shell heartbeat:

```sh
./codex/punch-loop.sh                       # poll every 5 min
PUNCH_LOOP_INTERVAL=120 ./codex/punch-loop.sh   # every 2 min
PUNCH_PROFILE=side ./codex/punch-loop.sh        # a specific board
```

Each tick runs a fresh `codex exec` for one batch and exits; the loop re-fires after the
interval. Fresh context per batch, indefinite pickup across batches.

## Notes & caveats
- **No MCP dependency.** Punchcard is just the `punch` CLI over shell, so Codex's
  non-interactive MCP-approval limitation (`codex exec` + MCP tools) never applies here.
- **Sandbox.** The worker edits and commits, and subagents create git worktrees under
  `$TMPDIR`. `punch-loop.sh` uses `--sandbox workspace-write`; a locked-down VM may need
  `--sandbox danger-full-access` (trusted environments only).
- **Concurrency** is server-controlled — `punch next --batch` hands out at most the board's
  cap regardless of Codex's `[agents] max_threads`. Leave `max_threads` at its default.
- **Cancel** is cooperative: subagents re-check `punch get <id>` at each checkpoint and
  abort if it left `in_progress`. Codex has no Claude-style instant pre-tool kill-switch
  hook, so `punch stop` / `punch pause` / **Cancel run** on the board are your governors —
  they halt claiming and cancel running tasks server-side, and the agent stops at its next
  checkpoint.
