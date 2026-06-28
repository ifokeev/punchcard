# punchcard

> Watch your AI coding agents ship — proof of work on every task, in one Go binary.

![punchcard board](docs/demo.gif)

Punchcard is a dead-simple **board** for solo devs who delegate to AI coding agents:
queue the work, then watch your agents claim each task, ship a PR, and leave **proof
of work** — a gif or screenshot — as receipts. Every task starts with **fresh
context** and runs on one static Go binary with **no database**, no build step, no
Electron.

## Why
Fast (local single binary, no cloud round-trips) and context-clean (each task runs in
a fresh subagent, not one ballooning chat). No database, no framework — just Go stdlib.

## Install the binary
```bash
curl -fsSL https://raw.githubusercontent.com/ifokeev/punchcard/main/install.sh | sh
punch serve                   # board + API on http://127.0.0.1:8080
```
Prefer source? `git clone https://github.com/ifokeev/punchcard && cd punchcard && go build -o punch .` (Go 1.22+).

## Use it with Claude Code
The binary is only half of it — the agents are driven by a Claude Code **plugin**
(two skills + a command). Install it:
```text
/plugin marketplace add ifokeev/punchcard
/plugin install punchcard@punchcard
```
> Needs the repo public. No marketplace? Copy `skills/*` → `~/.claude/skills/` and
> `commands/*` → `~/.claude/commands/`, then `/reload-plugins`. Run `/help` to see
> what's installed.

- **PM skill** — in your chat session, turns your intent into well-scoped task briefs
  and files them on the board.
- **Engineer** — ships each task as a reviewed PR with proof of work, a fresh subagent
  per task. Run it two ways:

| Goal | Run | Behavior |
|---|---|---|
| Clear what's queued, then stop | `/punch-loop` | One-shot — drains the current queue within one turn, then ends. |
| Stay on, pick up new tasks | `/loop 5m /punch-loop` | The built-in `/loop` re-fires `/punch-loop` every 5 min, so tasks the PM files later get picked up. `Esc` to stop. |

> `/punch-loop` is a normal command: it loops only *within a turn* and won't see tasks
> filed afterward — wrap it in the built-in `/loop` for a long-running worker.

Open the board and watch tasks slide from todo → done in real time. **Click any card**
for its full brief (description, acceptance criteria, PR, proof of work), or **drag a card
between columns** to set its status yourself. The header shows whether a loop is actually
polling — **active** / **idle** / **no loop** — and in-progress cards show the agent's
current step with time since its last activity, so you can tell a working run from a wedged
one at a glance (agents post steps with `punch update <id> --progress "<step>"`).

## Updating
The **binary** and the Claude Code **plugin** (the two skills + `/punch-loop`) update
independently.

**Binary** — re-run the install one-liner; it always fetches the latest release and
replaces the binary in place:
```sh
curl -fsSL https://raw.githubusercontent.com/ifokeev/punchcard/main/install.sh | sh
```
Check what you're on with `punch version` (also in the `punch serve` banner and the board header).

**Plugin (skills + command)** — from inside Claude Code:
```text
/plugin marketplace update punchcard     # refetch the latest from the marketplace
/plugin install punchcard@punchcard      # reinstall = update to that version
/reload-plugins                          # load it into the running session
```
Or scriptable from a shell (handy on a headless worker):
`claude plugin marketplace update punchcard && claude plugin install punchcard@punchcard`.

> **A running `/loop` won't pick up skill changes until you reload.** Skills load at
> session start or on `/reload-plugins`, so an in-flight worker keeps using the old skill
> until you run `/reload-plugins` (or `Esc` and restart the loop). Tasks already in
> progress finish under the old skill; the next batch uses the new one.

Installed the skills by hand (copy) or via `make dev` (symlinked clone)? Update the files
— re-copy, or `git pull` + `make dev` — then `/reload-plugins`.

## Control the run
Steer the loop from the **board** (or the `punch` CLI) without touching the agent
session — handy when the loop runs somewhere you can't reach:

| Action | Board | CLI |
|---|---|---|
| Cap how many agents run at once | **agents −/+** | `punch concurrency 3` |
| Pause / resume claiming | **Pause** / **Resume** | `punch pause` · `punch resume` |
| Cancel one running task | **Cancel run** on the card | `punch cancel <id>` |
| Kill-switch: stop everything now | **Stop all** | `punch stop` |

Concurrency is a **hard cap** — the server never lets more than that many tasks be in
progress at once (default **3**; seed a different default with `PUNCH_CONCURRENCY`).
**Pause is soft**: the loop idles and resumes when you un-pause from the board (no
terminal access needed). **Cancel** makes the owning agent abort at its next checkpoint
and moves the task to *Cancelled*. **Stop all** pauses, cancels every running task, and
(with the hook below) hard-halts the loop; clear it with **Resume**.

### Instant kill-switch (optional hook)
By default cancel is **cooperative** — a running agent stops at its next checkpoint,
not mid-edit. For an *instant* halt, enable the bundled **PreToolUse hook** by starting
the worker session with `PUNCH_KILLSWITCH=1`:
```bash
PUNCH_KILLSWITCH=1 claude        # then run /loop /punch-loop
```
Now **Stop all** (or `punch stop`) halts the loop on its very next tool call. The hook is
**opt-in** — it does nothing without that env var, so your other Claude sessions are
untouched — and **fail-open**: if the board is unreachable it never blocks your work.

## Task dependencies
Need one task **merged** before another starts? Declare it:
```bash
punch add --title "API on the new schema" --depends-on t_0001
punch add --title "Docs update" --depends-on t_0001,t_0002
```
The dependent sits in Todo (the board shows *waiting on t_0001*) and the loop won't claim
it until that dependency's PR is **actually merged** — not just marked done. Each tick the
loop reconciles real merge state with `gh` and flips merged dependencies, which unblocks
the dependents. So you can file a whole chain up front and let it land in order.

## Make it remote (pick one)
| Tier | How |
|---|---|
| Local | `punch serve` (binds 127.0.0.1) |
| Private mesh | `punch serve --addr 0.0.0.0:8080 --token $TOK` + Tailscale |
| Public, your own TLS | `punch serve --addr 127.0.0.1:8080 --token $TOK` behind Caddy/nginx (auto-HTTPS) |
| Public zero-trust | the above + Cloudflare Tunnel + Access |

> Binding a non-loopback address without `--token` is refused (pass `--insecure` to
> override). Behind a mesh the token is defense-in-depth; exposed with just your own
> TLS it's your **primary** auth — see [Public over your own TLS](#public-over-your-own-tls-no-mesh) below.
>
> With `--token` set, the CLI/loop send it as a bearer header; **opening the board in a
> browser prompts you for it** — leave the username blank and paste the token as the
> password (your browser remembers it for the session).

To point the `punch` CLI (and every agent subagent) at a remote or token-protected
board, run once:
```bash
punch config set --url https://your-board.example.com --token <token>
punch config show   # confirm settings
```
This writes `~/.punch/config.json` (mode 0600). All subsequent `punch` calls — including
those made by dispatched subagents — read the file automatically. The environment
variables `PUNCH_URL` and `PUNCH_TOKEN` always override the config file when set.

### Public over your own TLS (no mesh)
No Tailscale/Cloudflare? Then the **token is your only auth**, so two rules are
non-negotiable: a strong random token, and **HTTPS in front**. A bearer token over plain
`http://` on the public internet is sniffable, and whoever grabs it can drive your agents.

Run punch on loopback and let a reverse proxy terminate TLS — Caddy gets a cert
automatically:
```caddy
# Caddyfile — point a domain at the box first
board.example.com {
    reverse_proxy 127.0.0.1:8080
}
```
```bash
# on the server (run punch under systemd so it survives reboots)
SECRET=$(openssl rand -hex 32)            # treat it like a password
punch serve --addr 127.0.0.1:8080 --token "$SECRET"
```
Caddy forwards the `Authorization` header through unchanged, so the worker's **bearer**
token and your **browser Basic** login both reach punch. Point the worker at
`https://board.example.com` with that `$SECRET` (as above); rotate the token if it leaks.

### Multiple boards (profiles)
Running a board per project? Define named profiles and switch with one env var:
```bash
punch config set --profile work --url https://work.example.com --token <tok>
punch config set --profile side --url https://side.example.com --token <tok>
punch config use work          # set the default
punch config list              # see them all (* = active)
```
Pick a profile per worker session with `PUNCH_PROFILE` (a `.envrc` per repo is handy):
```bash
PUNCH_PROFILE=side claude       # then /loop /punch-loop → drives the side board
```
That one var routes the loop, every subagent, and the kill-switch hook to that board, so
two projects run side by side without colliding. `PUNCH_URL`/`PUNCH_TOKEN` still win over
a profile.

## Move a board between instances
Everything is in `tasks.json` + `memory.json`, so a **local** move is just copying those
files (and `artifacts/`). For a **remote** board you can't reach by filesystem, do it over
the API:
```bash
punch export --out board.json                  # from the source board
PUNCH_PROFILE=newbox punch import board.json    # into the target board
```
The bundle carries tasks **and** memory with full state — status, dependencies, merge
flags, PR links. Import **refuses a non-empty board unless you pass `--replace`**, so it
can't silently clobber existing work. Proof-of-work artifacts (binary) aren't bundled —
copy the `artifacts/` directory alongside if you want them.
