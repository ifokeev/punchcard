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
(Positioning note: lead with proof-of-work + watch-it-ship + no-DB; don't call it an
"orchestrator" or "task tracker" — see project memory.)

## Quickstart
```bash
go install punchcard@latest   # or grab a release binary
punch serve                   # board + API on http://127.0.0.1:8080
```
Open the board, add a task. In one Claude Code session use the **PM** skill to file
tasks; in another run `/loop` with the **Engineer** skill to ship them.

## Make it remote (pick one)
| Tier | How |
|---|---|
| Local | `punch serve` (binds 127.0.0.1) |
| Private mesh | `punch serve --addr 0.0.0.0:8080 --token $TOK` + Tailscale |
| Public zero-trust | the above + Cloudflare Tunnel + Access |

> Binding a non-loopback address without `--token` is refused (pass `--insecure` to
> override). Put Tailscale/Cloudflare in front; the bearer token is defense-in-depth.

## Pair with
[graphify](https://github.com/safishamsi/graphify) for a codebase map — punchcard is
the work memory, graphify is the code map.
