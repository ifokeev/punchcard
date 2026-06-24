# Contributing to punchcard

Thanks for considering a contribution! punchcard is deliberately small — a single
static Go binary with **no third-party dependencies** plus a couple of Claude Code
skills. Keep changes in that spirit: minimal, stdlib-first.

## Build & test
Requires **Go 1.22+**.
```bash
git clone https://github.com/ifokeev/punchcard && cd punchcard
go build -o punch .        # build the binary
go test ./...              # run the tests
go vet ./...               # static checks
make release               # cross-compile dist/ binaries (optional)
```
Run it: `./punch serve`, then open <http://127.0.0.1:8080>.

**Fast dev setup — and update:** `make dev` builds + installs the `punch` binary and
**symlinks** the Claude Code skills + `/punch-loop` into `~/.claude` (so repo edits
apply live). Re-run it any time to rebuild/refresh. Then run `/reload-plugins` in
Claude Code.
```bash
make dev                                  # install into $(go env GOPATH)/bin + ~/.claude
make dev BINDIR=/usr/local/bin            # override the binary location (must be on PATH)
make dev-uninstall                        # remove the binary + linked skills/command
```

## Ground rules
- **Stdlib only.** No new Go modules — if you reach for a dependency, stop; the
  stdlib almost certainly covers it. The single static `CGO_ENABLED=0` binary is the
  whole point.
- **Tests for logic.** Every non-trivial path gets a Go test (see the `*_test.go`
  files).
- **Conventional commits.** `feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, …
- **Keep it simple.** Prefer the smallest change that works; new surface area needs a
  clear reason.

## Project layout
```
*.go                       the binary: store, server, API, CLI, uploads, memory, config
board.html                 the embedded board UI
skills/                    Claude Code PM + Engineer skills
commands/punch-loop.md     the /punch-loop command
hooks/                     SessionStart hook — reminds (never installs) if agent-browser is missing
.claude-plugin/            plugin + marketplace manifests
examples/                  demo seed + live demo driver
Makefile                   build / cross-compile
```

## Pull requests
1. Fork, branch, make your change **with tests**.
2. `go build ./... && go vet ./... && go test ./...` must pass.
3. Open a PR describing the *what* and *why*. Small, focused PRs merge fastest.

## License
By contributing, you agree your contributions are licensed under the
[MIT License](LICENSE).
