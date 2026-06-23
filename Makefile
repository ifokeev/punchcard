BINARY=punch
LDFLAGS=-s -w
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

.PHONY: build test release
