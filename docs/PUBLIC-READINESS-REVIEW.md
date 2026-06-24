# Punchcard — Public-Readiness Review

> Open-source / public-launch readiness assessment of **punchcard** (a Go HTTP task
> board + `punch` CLI; repo `github.com/ifokeev/punchcard`). Review-only: no code
> behavior was changed. All build/test/vet results below were produced by actually
> running the commands on this checkout.

**Date:** 2026-06-24
**Reviewed commit:** `origin/main` @ `8f2121e`
**Toolchain:** `go version go1.26.3 darwin/arm64`

---

## Verdict at a glance

| # | Dimension | Status |
|---|---|---|
| 1 | Docs | **Ready** |
| 2 | License & legal | **Ready** |
| 3 | Security | **Ready** (one minor hardening note) |
| 4 | Tests & CI | **Needs work** (no CI; tests/build/vet pass) |
| 5 | Code quality | **Ready** (t_0002 flag bug already fixed) |
| 6 | API surface & UX | **Needs work** (no `/health` — t_0001) |
| 7 | Repo hygiene | **Ready** |

**Overall: GO with two pre-launch nits** — ship-able now; close t_0001 (`/health`) and
add a minimal CI workflow to make it fully polished. Neither is a blocker.

---

## 1. Docs — **Ready**

A newcomer can go from zero to a running board in well under 5 minutes.

- `README.md:17-22` — one-line curl install (`install.sh`) **and** a from-source path
  (`go build -o punch .`, Go 1.22+). `punch serve` is documented to bind
  `http://127.0.0.1:8080`.
- `README.md:24-49` — Claude Code plugin install, PM vs Engineer skills, and the
  `/punch-loop` vs `/loop 5m /punch-loop` distinction laid out in a table.
- `README.md:51-78` — run-control (pause/resume, concurrency cap, cancel, kill-switch)
  documented for both board and CLI, including the opt-in `PUNCH_KILLSWITCH=1` hook.
- `README.md:80-98` — three-tier remote story (local / Tailscale mesh / Cloudflare
  zero-trust) and `punch config set` for pointing the CLI at a remote board.
- `CONTRIBUTING.md:7-53` — build/test/vet commands, the stdlib-only ground rule, the
  `make dev` symlink workflow, conventional-commit expectation, and an accurate project
  layout map.
- `docs/demo.gif` exists and is referenced at `README.md:5`.

Minor (non-blocking): the README references the curl-from-`main` installer and the
`/plugin marketplace add` flow, both of which require the repo to actually be public —
fine the moment it is published.

## 2. License & legal — **Ready**

- `LICENSE:1-3` — MIT, `Copyright (c) 2026 ifokeev`. Standard, correct text.
- `CONTRIBUTING.md:55-57` — inbound contributions explicitly licensed under MIT.
- No proprietary or leftover absolute paths in tracked source. The only `/Users/...`
  hit is `.git` (the worktree gitdir pointer used for this review — not a tracked file).
- No personal data (no `ivan@`, no `mlcraft`, etc.) in any tracked `.go`/`.md`/`.json`.
- `go.mod:1` module path is `punchcard` (a short local path, not a full
  `github.com/...` path). Acceptable for a `main`-package binary that ships no importable
  library, but see the note in §7.

## 3. Security — **Ready** (one minor hardening note)

Auth and safe-default posture are well thought through.

- **Token comparison is constant-time.** `middleware.go:19` uses
  `subtle.ConstantTimeCompare` against the full `"Bearer " + token` string — no timing
  side channel, auth correctly disabled when token is empty (`middleware.go:13`).
- **Safe bind default.** `cmdServe` defaults `--addr` to `127.0.0.1:8080`
  (`main.go:57`). `validateBind` (`middleware.go:41-56`) **refuses** a non-loopback bind
  when no `--token` is set unless `--insecure` is passed, and correctly treats an empty
  host (bind-all) as non-loopback (`middleware.go:47-48`). Covered by
  `TestValidateBindFailSafe` (`middleware_test.go:54`).
- **Proxy header hardening.** `X-Forwarded-*` headers are stripped unless
  `--trusted-proxy` is set (`middleware.go:28-39`), so a client can't spoof
  scheme/host for the absolute-URL builder (`upload.go:23-38`).
- **Upload safety.** Size capped *before* parse via `http.MaxBytesReader`
  (`upload.go:47`); filenames and task IDs sanitized through `safeName`
  (`upload.go:13-19`, `upload.go:54-55`); artifacts served read-only with
  `Content-Disposition: attachment` + `X-Content-Type-Options: nosniff`
  (`upload.go:82-83`) so attacker HTML/SVG can't execute. Inline preview is restricted
  to an image/video extension allowlist (`server_board.go:22-28`).
- **Config secrecy.** `~/.punch/config.json` is written `0600` and its dir `0700`
  (`config.go:49-56`); tokens are masked on display (`main.go:419-424`).
- No real hardcoded secrets — the only `secret`/`supersecret` strings are test fixtures
  (`middleware_test.go:14`, `config_test.go:16`).

Minor hardening note (non-blocking): per-request `Authorization` is checked, but there
is no rate limiting on the auth middleware. For the documented deployment model
(loopback, or behind Tailscale/Cloudflare Access) this is acceptable defense-in-depth;
worth a README sentence if direct internet exposure is ever encouraged.

## 4. Tests & CI — **Needs work** (CI absent; build/test/vet pass clean)

Evidence — commands run on this checkout:

```
$ go build ./...
build exit=0

$ go vet ./...
vet exit=0

$ go test ./...
ok  	punchcard	1.298s
test exit=0
```

- **38 test functions** across 9 `*_test.go` files (`cli_test.go`, `config_test.go`,
  `control_test.go`, `memory_test.go`, `middleware_test.go`, `server_board_test.go`,
  `server_test.go`, `store_test.go`, `upload_test.go`). Core paths are well covered:
  claim ordering and the no-double-claim guarantee (`store_test.go:113`
  `TestClaimAtomicNoDoubleClaim`), concurrency ceiling (`store_test.go:97`
  `TestClaimBatchCeiling`), the API surface end-to-end (`server_test.go`), invalid-status
  rejection (`server_test.go:171` `TestPatchRejectsInvalidStatus`), upload traversal
  defense (`upload_test.go:14` `TestSafeNameStripsTraversal`), and the CLI flag-order fix
  (`cli_test.go:12` `TestCmdUpdateFlagsAfterID`).

**Gap: there is no CI.** `.github/` does not exist in the repo — no
`.github/workflows/`, so nothing runs `go build/vet/test` on PRs. `CONTRIBUTING.md:52`
asks contributors to run these manually, but that's unenforced. For a public repo
inviting PRs, a one-file GitHub Actions workflow (checkout → setup-go → build/vet/test)
is the single highest-value addition. **Not a launch blocker, but strongly recommended.**

## 5. Code quality — **Ready** (t_0002 already fixed)

- **t_0002 (flag-order no-op) is FIXED and tested.** `cmdUpdate` takes the id as
  `args[0]` *then* parses flags from `args[1:]` — `id := args[0]` (`main.go:211`),
  `fs.Parse(args[1:])` (`main.go:217`) — with the rationale comment on `main.go:211`.
  `cmdMemorySearch` uses the same positional-first pattern (`main.go:345-348`). Regression
  test: `cli_test.go:12` `TestCmdUpdateFlagsAfterID`. So `punch update <id> --status ...`
  no longer silently no-ops.
- **Error handling is consistent and rollback-safe.** Every mutating store op writes via
  an atomic temp-file + `fsync` + `rename` (`store.go:99-129`, `control.go:98-122`) and
  **rolls back the in-memory change on flush failure** — e.g. `Claim` (`store.go:148-151`),
  `ClaimBatch` (`store.go:203-208`), `Patch` (`store.go:253-256`), `CancelInProgress`
  (`store.go:339-344`), `Create` (`store.go:297-300`). This avoids in-memory/on-disk
  divergence.
- **Status is a closed validated set.** `validStatuses` (`store.go:46-51`) plus API
  validation (`server.go:119-122`) ensure a typo can't drop a card off every column.
- No `TODO`/`FIXME`/`HACK`/`ponytail` debt markers in source. The only `XXX` hit is the
  literal `"last 4: XXXX"` in the token-mask docstring (`main.go:418`).
- CLI traps: subcommands fail fast with usage strings and non-zero exit
  (`client.go:101-104` `fail`), and `cmdNext` returns distinct exit codes for
  drained-queue (`3`) vs paused (`4`) so the loop can idle instead of stop
  (`main.go:128-133`) — a deliberate, well-commented design.

## 6. API surface & UX — **Needs work** (no `/health` — t_0001)

Routes are consistent and RESTful (`server.go:16-229`): `GET/POST /api/tasks`,
`GET/PATCH/DELETE /api/tasks/{id}`, `POST /api/next`, `POST /api/cancel-all`,
`GET/PATCH /api/control`, the memory CRUD set, plus upload + board routes. Error
responses are uniform `http.Error` text with correct status codes, and `/api/next`
cleverly uses `423 Locked` to mean "paused, idle don't stop" (`server.go:57-61`).

**Gap: there is no `/health` (or `/healthz`) endpoint.** Confirmed by grep — `health`
appears only in the demo scripts (`examples/demo-seed.sh:12-14`,
`examples/demo-drive.sh:23`), which seed a *task* asking an agent to add one; it is not
implemented in `server.go`. This is exactly **task t_0001**. For public/remote
deployments (the Tailscale/Cloudflare tiers in `README.md:80-88`), a liveness probe is a
near-universal expectation. **Not a hard blocker** — `GET /api/tasks` works as a crude
liveness check — but closing t_0001 is the right call before promoting remote use.

Graceful failure when store unavailable: the stores are loaded **once at startup**
(`main.go:73-87`) and a load error aborts boot via `fail(...)` rather than serving a
half-broken process — the correct fail-fast posture. At request time the store is
in-memory, so the realistic failure is a *save* failure, which is handled by rollback +
`500` (§5). Adequate.

## 7. Repo hygiene — **Ready**

- `.gitignore:1-13` is correct: planning artifacts (`docs/superpowers/`) are ignored per
  project rules, as are build outputs (`/punch`, `/punchcard`, `/dist/`) and all runtime
  data (`/tasks.json`, `/memory.json`, `/control.json`, `/artifacts/`).
- `git ls-files` shows **no** committed runtime JSON, build binaries, `dist/`, or
  `artifacts/` — nothing leaked.
- Tracked `docs/` contains only `demo.gif` (the README asset); no stray planning docs.
- Module path `punchcard` (`go.mod:1`) is sensible for a single-binary `main` package.
  Minor (non-blocking): if any package here is ever meant to be `go install`-able or
  importable, switch to the full `github.com/ifokeev/punchcard` path; for the current
  ship-a-binary model the short path is fine.

---

## GO / NO-GO

### Verdict: **GO** (conditional polish, no blockers)

The codebase is clean, well-tested (38 tests, all green), and `go build`, `go vet`, and
`go test ./...` all pass on this checkout. Security defaults are sound (loopback bind,
token refusal on non-loopback, constant-time auth, upload hardening). License and repo
hygiene are correct. The previously-known CLI flag-order bug (**t_0002**) is already
fixed and regression-tested. Nothing here blocks a public launch.

Two items keep this from being a *flawless* launch and should be closed promptly — but
both are "needs work," not "blocker."

### Prioritized fix list

1. **P1 — Add a CI workflow** (dimension 4). Create
   `.github/workflows/ci.yml`: checkout → `setup-go` (1.22) → `go build ./...` →
   `go vet ./...` → `go test ./...` on push/PR. `.github/` does not currently exist.
   Highest value for a public repo accepting PRs; enforces the rules `CONTRIBUTING.md:52`
   only states.
2. **P2 — Implement `GET /health`** (dimension 6 / **task t_0001**). Add a trivial
   `200 ok` route in `newMux` (`server.go:16`). Currently only referenced by the demo
   seed scripts; needed for the documented remote/zero-trust deployment tiers.
3. **P3 — README note on rate limiting / direct-exposure caveat** (dimension 3, minor).
   State that the bearer token is defense-in-depth and direct internet exposure should
   sit behind Tailscale/Cloudflare Access (already implied at `README.md:86-88`; make the
   "no built-in rate limiting" explicit).
4. **P4 — Consider full module path** (dimension 7, optional). Only if any package is to
   become importable; irrelevant for the current ship-a-binary model.

**Already resolved (no action):** t_0002 flag-order bug — fixed at `main.go:211-217`,
covered by `cli_test.go:12`.
