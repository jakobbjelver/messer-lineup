BINARY  := lineup
PKG     := github.com/jakobbjelver/messer-lineup
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.BuildDate=$(DATE)

.PHONY: all build dev test race fmt fmt-check vet lint check public-scan clock-check tidy clean doctor verify mine smoke probe

all: build

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/lineup

# `lineup serve` arrives in P3 with the API and the board; until then the dev pair has nothing to
# start, and the target says so instead of starting something that answers nothing.
dev:
	@echo "dev: nothing to serve yet — lineup serve lands in P3 (plan: .hermes/plans/2026-11-17_start-here-p0.md)" >&2
	@exit 1

doctor: build
	@./bin/$(BINARY) doctor $(ARGS)

verify: build
	@./bin/$(BINARY) verify $(ARGS)

# A sweep. ARGS names the towns: make mine ARGS="--town berlin,london --dry-run --report r.json"
mine: build
	@./bin/$(BINARY) mine $(ARGS)

# The header-profile canary. It touches the network, so it is not in check; run it before believing any
# report that a source has started blocking us (UA-only evidence is a known false "blocked").
smoke:
	@go test -tags livesmoke -run TestLiveHeaderProfile ./internal/fetch/ -v

# What can this egress reach, and in what form. The numbers in docs/coverage.md come from here.
probe:
	@go test -tags livesmoke -run 'TestLive(Reachability|EventimDetail|PlatformSurvey)' ./internal/fetch/ -v

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

# The clock boundary: time.Now() may appear in internal/clock and nowhere else, because every selection
# path takes the time as a parameter and a function that reads the clock cannot be asserted on.
clock-check:
	@hits=$$(git grep -nE "time\.Now\b" -- '*.go' ':!internal/clock/' || true); \
	if [ -n "$$hits" ]; then echo "clock: time.Now() outside internal/clock:"; echo "$$hits"; exit 1; fi

check: fmt-check vet test race public-scan clock-check

tidy:
	go mod tidy

clean:
	rm -rf bin
