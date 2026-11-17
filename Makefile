BINARY  := lineup
PKG     := github.com/jakobbjelver/messer-lineup
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.BuildDate=$(DATE)

.PHONY: all build dev test race fmt fmt-check vet lint check public-scan tidy clean

all: build

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/lineup

# `lineup serve` arrives in P0.5; until then there is nothing to serve and the target says so
# instead of starting something that answers nothing.
dev:
	@echo "dev: nothing to serve yet — lineup serve lands in P0.5 (plan: .hermes/plans/2026-11-17_start-here-p0.md)" >&2
	@exit 1

# The frontend joins this Makefile at P4 as `web`, and build/test gain it as a prerequisite
# (`//go:embed all:app/dist` makes the SPA a build input).

test:
	go test ./...

race:
	go test -race ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	golangci-lint run

# fmt-check fails instead of rewriting: this is what CI runs.
fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# The repo is public: shipped files must not name the personal estate or a host path (STANDARD §11.1).
# The character classes ('/User[s]/') keep this target from matching its own recipe; the nightly audit
# checks the same set from outside the repo, which is what makes the trick a readability device rather
# than a loophole.
public-scan:
	@pat='cloudflare acces[s]|tailscal[e]|zerotie[r]|/User[s]/|/Volume[s]/|C:\\User[s]\\|@bjelve[r]'; \
	hits=$$(git grep -nIiE "$$pat" || true); \
	if [ -n "$$hits" ]; then echo "PUBLIC_SCAN: the tree names the estate or a host path:"; echo "$$hits"; exit 1; fi

check: fmt-check vet test race public-scan

tidy:
	go mod tidy

clean:
	rm -rf bin
