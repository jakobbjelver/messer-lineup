# AGENTS.md — Messer Lineup

A season board for live music: a private miner publishes a static index of shows, and a small edge
deployment serves the board and its API from that index.

## What this is

Lineup answers one question — *which of the artists I listen to are playing near me, soon* — without
an account. The miner crawls event platforms country by country on the machine that owns the crawl,
publishes a nine-key projection of each show, and nothing else: the read path is static files.
The unusual part is the discipline that follows from that: the published row is an allowlist a test
enforces, the clock is always a parameter, and both serving runtimes are held to one fixture file
instead of one document.

## Read first

- `PROJECT.yaml` — the machine-readable facts (tier, stack, deploy, ports, exceptions).
- `README.md` — how to run it.
- `docs/coverage.md` — what each event source actually measures, per country (lands with the first
  crawl).
- `docs/decisions/` — the ADRs; the index hosting and row contract is the one to read first.
- Skill `messer-lineup` (agent-side, not in this tree) — live state, diagnosis ladders and traps.
- The full build plan is kept outside the public tree; `docs/coverage.md` is the record that ships.

## Layout

| Path | What |
|---|---|
| `cmd/lineup/` | the entry point: `verify`, `doctor`, and the commands the plan has not reached yet |
| `internal/model/` | `Show` (full fidelity), `Row` (the published nine keys), identity keys |
| `internal/clock/` | the only place the wall clock is read; every selection path takes the time as a parameter |
| `internal/index/` | the read side of the published format: manifest, picker, shards, the row contract |
| `internal/doctor/` | one report on the state of an install, without changing anything |
| `internal/fetch/` | the HTTP client: browser header profile, explicit gzip, per-host limiter, robots cache |
| `internal/sources/` | one adapter per platform, plus the registry loader (P0.7+) |
| `internal/board/` | the board selector: one pure function, the clock injected (P5) |
| `sources/` | the registry, data only — `countries/*.toml`, `aliases/*.toml` |
| `web/` | the board; built to `web/dist` and served as static assets |
| `wrangler.jsonc` | the one deployment: assets first, script only for `/api/v1/*` and `/calendar/*` |

## Commands

```bash
make build     # bin/lineup
make verify    # check a built index; ARGS="--index <dir>" to point it elsewhere
make doctor    # report the install: version, go, clock, index state, registry
make dev       # the local pair (lands in P3 with `lineup serve`)
make test      # unit + integration
make race      # the same under -race
make lint      # golangci-lint
make check     # the full local gate: fmt-check, vet, test, race, public-scan, clock-check
```

Every command is non-interactive and exits non-zero on failure. A command that only works on one
machine is a bug.

## Invariants

- **A `Show` cannot become a `Row` with a price in it.** The projection is structural: `Row` has nine
  keys and no content fields, and a test marshals a full show through it and asserts `p`, `cur`,
  `desc`, `img`, `lineUp` and `seats` are absent.
- **No selection path reads the clock.** `internal/board` and `web/src/lib/board.ts` take the time as
  a parameter, because every user-input scenario asserts a date window. `make clock-check` fails the
  build if the wall clock is read anywhere outside `internal/clock`.
- **The identity vector file is canonical.** `internal/model/testdata/keys.json` is never edited to
  make an implementation pass, and Go and TypeScript agree on every vector.
- **Assets are served before the Worker.** `run_worker_first` is never set in `wrangler.jsonc`; a
  request that always reaches the script is metered, and a page view is supposed to cost nothing.
- **Nothing in the read path is a database.** The board reads published index files; a stateful store
  only exists for sessions and taste.
- **The miner runs where the crawl runs.** No API keys in the serving deployment, no accounts.

## Working rules

- The published index is a projection, never a mirror: no prices, no descriptions, no images, no
  archive. `lineup verify` refuses an index that breaks the rules.
- A source that answers 0 is investigated with the full browser header profile before it is recorded
  as blocked — a TLS reset on a UA-only request is not evidence of anything.
- Delete the old layer in the same change that ships the new one. No compatibility shims.
- Update `PROJECT.yaml`, this file and the skill in the same PR as a structural change.
- `sources/` is data; community contributions touch it and nothing else.

## Where the depth is

`docs/decisions/` — the ADRs behind the index hosting, the row contract, the tier and the
publication posture. The full build plan is agent-side; this file and the ADRs are the contract that
ships.
