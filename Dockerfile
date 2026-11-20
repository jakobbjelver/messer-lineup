# syntax=docker/dockerfile:1
#
# Messer Lineup, self-hosted. One image: the board built as static files, the Go binary that serves them and the
# API beside them. **Nothing is `go:embed`-ed** — `web/dist` sits next to the binary on disk, exactly as
# `lineup serve` does on the mini, so the same files are a directory and an image.
#
# The index is **not** baked. It is a publish artifact that changes nightly (manifest + content-addressed shards),
# so it is a volume: mount a directory written by `lineup mine` and the image serves it.
#
#   docker build -t messer-lineup .
#   docker run -p 8080:8080 -v /srv/lineup/index:/data/index messer-lineup
#
# The Last.fm taste import is optional: pass `-e LASTFM_KEY=…` and the route answers; without it the route says
# `not_configured` instead of pretending the provider failed.

# ---- the board: same-origin build, because this image answers /manifest.json itself ----
# A glibc base, not alpine: rollup's native parser ships prebuilt bindings per platform, and without the linux
# musl build npm falls back to the wasm parser, which dies on real-world bundles with "code is nested too deeply".
FROM node:22-bookworm-slim AS board
WORKDIR /build
COPY web/package.json web/package-lock.json ./web/
# The board imports the calendar token's spec from the repo root (`src/calendar/spec.ts`) — one implementation for
# the client, the Worker and this build — so the layout has to match the repo, not just `web/`.
COPY src/ ./src/
COPY web/ ./web/
RUN cd web && npm ci --include=optional && npm run build

# ---- the binary ----
FROM golang:1.26-alpine AS api
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/lineup ./cmd/lineup

# ---- the runtime: no shell scripts, no sidecar, one non-root process ----
FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 lineup
WORKDIR /app

COPY --from=api /out/lineup /usr/local/bin/lineup
COPY --from=board /build/web/dist /app/web
# The music policy ships with the binary that reads it: a self-hosted miner and a self-hosted server must agree on
# which rooms are music.
COPY sources/ /app/sources/

# Defaults are baked here; compose carries only secrets and the things that differ per host. `lineup serve` reads
# these names itself, so no entrypoint script is needed to translate them into flags.
ENV LINEUP_INDEX=/data/index \
    LINEUP_WEB=/app/web \
    LINEUP_ADDR=0.0.0.0:8080

EXPOSE 8080
USER lineup

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/lineup", "serve"]
