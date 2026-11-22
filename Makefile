BINARY  := lineup
PKG     := github.com/jakobbjelver/messer-lineup
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.BuildDate=$(DATE)

.PHONY: probe-board csp all build dev test race fmt fmt-check vet lint check public-scan clock-check tidy clean doctor verify mine serve smoke probe web web-edge web-dev worker-dev worker-check keycheck geocheck contract

all: build

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/lineup

# The Go runtime: the index, the API envelope, and the board once web/dist exists.
dev: serve

doctor: build
	@./bin/$(BINARY) doctor $(ARGS)

verify: build
	@./bin/$(BINARY) verify $(ARGS)

# A sweep. ARGS names the towns: make mine ARGS="--town berlin,london --dry-run --report r.json"
mine: build
	@./bin/$(BINARY) mine $(ARGS)

# Serve the built index (and the board, once web/dist exists). ARGS for --addr/--web/--now.
serve: build
	@./bin/$(BINARY) serve $(ARGS)

# The board. `make serve` must be running: the dev server proxies the three index paths to it.
web:
	@cd web && npm run --silent build

# The edge build: the index is on its own host, so the board must be told where. Deploy this one, never the
# same-origin build — that one asks the Worker for /manifest.json, which is not a file and not a route.
web-edge:
	@cd web && VITE_DATA_BASE=$${DATA_BASE:-https://lineup-data.messer.wtf} npm run --silent build

# The CSP's inline-script hash is a copy of the built page: rewrite it after any edit to index.html's theme bootstrap.
csp:
	@node web/scripts/cspcheck.mjs --write

web-dev:
	@cd web && npm run dev

# The Worker, locally. Assets are required by the config, so a stub is created when the board has not been
# built yet — labelled as a stub so it can never be mistaken for the real page.
worker-dev:
	@mkdir -p web/dist
	@test -f web/dist/index.html || printf '%s\n' \
		'<!doctype html><title>Messer Lineup</title>' \
		'<body style="font:14px system-ui;padding:2rem">The board is not built yet. Run <code>make web</code> first, or <code>make serve</code> for the index alone.</body>' \
		> web/dist/index.html
	@npx wrangler dev --port 8788

# Typecheck the Worker. `make check` stays Go-only so CI needs no node_modules.
worker-check:
	@test -d node_modules || { echo "worker-check: run npm install first" >&2; exit 1; }
	@npm run --silent check

# The identity contract, in the second runtime: the TypeScript twin of model.Key against the shared vectors.
keycheck:
	@node web/scripts/keycheck.mjs

# The distance contract: the radius the edge uses, the pairs a source actually emits, and the formatter's widths.
geocheck:
	@node web/scripts/geocheck.mjs

# The design gates, against a generated fixture index (the probe needs a known board, and a board is a window).
# `web` first: the probe serves the same-origin bundle, and `web-edge` (what deploys) asks the bucket for its index.
probe-board: build web
	@go run ./scripts/fixture -dir testdata/index
	@./bin/lineup serve --index testdata/index --web web/dist --addr 127.0.0.1:8016 >/tmp/lineup-probe.log 2>&1 & pid=$$!; \
	trap "kill $$pid 2>/dev/null" EXIT; \
	for i in $$(seq 1 60); do curl -sf http://127.0.0.1:8016/healthz >/dev/null && break; sleep .2; done; \
	node web/scripts/probe-board.mjs http://127.0.0.1:8016 --town stockholm-se

# G1: the same requests against both runtimes. Needs `make serve` and `make worker-dev` running.
contract:
	@node scripts/contract.mjs

# Ship it: the edge build, then the Worker. `web-edge` is not optional — the same-origin bundle (what `make web`
# builds, for `lineup serve`) asks the Worker for /manifest.json, which is neither a file nor a route, so the board
# renders its error state on a perfectly healthy origin. Deploying is a `main` action: the tree that ships is the
# tree that was committed.
deploy: web-edge dist-edge-check
	@node web/scripts/cspcheck.mjs
	@npx wrangler deploy

# The wrong build is silent: the same-origin bundle answers 200 from the SPA fallback for /manifest.json, so the
# board renders "Couldn't load the board. JSON.parse: unexpected character at line 1 column 1" on a healthy origin.
# Assert the shipped bytes name the bucket before anything is uploaded.
dist-edge-check:
	@grep -q 'lineup-data\.messer\.wtf' web/dist/assets/*.js || { \
		echo "deploy: web/dist is the same-origin build (no bucket host in the bundle). Run make web-edge." >&2; exit 1; }

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

# The wall clock lives in internal/clock and nowhere else, because every selection path takes the time as a
# parameter and a function that reads the clock cannot be asserted on. The pattern is bare on purpose: an earlier
# version used `time\.Now\b`, and on this host's BSD regex `\b` is a backspace character — so the gate matched
# nothing, passed locally, and failed only on Linux CI. The self-test below proves the pattern still catches a
# known-bad sample, which is the only way a grep gate can be trusted.
clock-check:
	@pat='time\.Now'; \
	printf '%s\n' 'started := time.Now()' | grep -qE "$$pat" || { echo "clock: the pattern no longer matches a known-bad sample"; exit 1; }; \
	hits=$$(git grep -nE "$$pat" -- '*.go' | grep -v '^internal/clock/' || true); \
	if [ -n "$$hits" ]; then echo "clock: wall-clock read outside internal/clock:"; echo "$$hits"; exit 1; fi

check: fmt-check vet test race public-scan clock-check

tidy:
	go mod tidy

clean:
	rm -rf bin
