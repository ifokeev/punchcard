# punchcard

> Watch your AI coding agents ship — proof of work on every task, in one Go binary.

Punchcard is a dead-simple **board** for solo devs who delegate to AI coding agents:
queue the work, then watch your agents claim each task, ship a PR, and leave **proof
of work** — a gif or screenshot — as receipts. Every task starts with **fresh
context** and runs on one static Go binary with **no database**, no build step, no
Electron.

## Why
Fast (local single binary, no cloud round-trips) and context-clean (each task runs in
a fresh subagent, not one ballooning chat). No database, no framework — just Go stdlib.

## Install
```bash
curl -fsSL https://raw.githubusercontent.com/ifokeev/punchcard/main/install.sh | sh
punch serve                   # board + API on http://127.0.0.1:8080
```
Prefer source? `git clone https://github.com/ifokeev/punchcard && cd punchcard && go build -o punch .` (Go 1.22+).

## Drive your agents (Claude Code)
punchcard ships as a Claude Code plugin with two roles:
- **PM** — in your chat session, the `punchcard-pm` skill turns your intent into
  well-scoped task briefs and files them on the board.
- **Engineer** — in a worker session, run **`/punch-loop`**: it claims each task, spins
  up a fresh subagent to implement → open a PR → self-review → attach proof of work,
  then moves on until the board is clear.

Open the board and watch tasks slide from todo → done in real time.

## Make it remote (pick one)
| Tier | How |
|---|---|
| Local | `punch serve` (binds 127.0.0.1) |
| Private mesh | `punch serve --addr 0.0.0.0:8080 --token $TOK` + Tailscale |
| Public zero-trust | the above + Cloudflare Tunnel + Access |

> Binding a non-loopback address without `--token` is refused (pass `--insecure` to
> override). Put Tailscale/Cloudflare in front; the bearer token is defense-in-depth.
