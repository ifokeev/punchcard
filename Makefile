BINARY=punch
# VERSION comes from the git tag (e.g. v0.6.1); falls back to short SHA, then "dev".
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS=-s -w -X main.version=$(VERSION)
TARGETS=linux/amd64 linux/arm64 darwin/arm64 darwin/amd64

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	go test -race ./...

release: test
	@mkdir -p dist
	@for t in $(TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; \
	  echo "building $$os/$$arch"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
	    -o dist/$(BINARY)-$$os-$$arch .; \
	done

BINDIR ?= $(shell go env GOPATH)/bin
CLAUDE_DIR ?= $(HOME)/.claude

# Dev setup AND update in one idempotent command: build + install the `punch` binary
# and link the Claude Code skills + /punch-loop into ~/.claude. Re-run any time to update.
# Override the binary location (must be on your PATH) with: make dev BINDIR=/usr/local/bin
dev: build
	@mkdir -p "$(BINDIR)" "$(CLAUDE_DIR)/skills" "$(CLAUDE_DIR)/commands"
	@cp $(BINARY) "$(BINDIR)/$(BINARY)"
	@ln -sfn "$(CURDIR)/skills/punchcard-pm" "$(CLAUDE_DIR)/skills/punchcard-pm"
	@ln -sfn "$(CURDIR)/skills/punchcard-engineer" "$(CLAUDE_DIR)/skills/punchcard-engineer"
	@ln -sfn "$(CURDIR)/commands/punch-loop.md" "$(CLAUDE_DIR)/commands/punch-loop.md"
	@echo "punch installed -> $(BINDIR)/$(BINARY)  (ensure $(BINDIR) is on your PATH)"
	@echo "skills + /punch-loop linked -> $(CLAUDE_DIR)  (symlinked: repo edits apply live)"
	@echo "next: run /reload-plugins in Claude Code, then  punch serve"

dev-uninstall:
	@rm -f "$(BINDIR)/$(BINARY)" \
	  "$(CLAUDE_DIR)/skills/punchcard-pm" \
	  "$(CLAUDE_DIR)/skills/punchcard-engineer" \
	  "$(CLAUDE_DIR)/commands/punch-loop.md"
	@echo "uninstalled: removed binary + linked skills/command"

.PHONY: build test release dev dev-uninstall
