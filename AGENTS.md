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
| `internal/server/` | serves a built index with the edge's headers, `/data/<town>.json` for the SPA, health, and a 501 in the contract's envelope for the routes that need accounts |
| `internal/publish/` | copies a verified index into R2 with SigV4, skipping content-addressed objects that already exist |
| `web/src/lib/{key,taste}.ts` | the client's identity function (the twin of `model.Key`) and the pasted-list parser; `make keycheck` diffs the twin against the shared vectors |
| `web/src/lib/{geo,location}.ts` | distance (the edge's own radius), the position rules, and the GPS hook whose fix never leaves memory; `make geocheck` asserts the numbers |
| `web/` | the board: Vite + React 19, the token sheet (`styles/tokens.css`) and the board's own CSS, a two-step first-run gate, the day-grouped list, the month calendar, and a loader that reads `manifest.json` + one shard |
| `src/worker.ts` | the Worker (Hono): `/healthz`, `/readyz`, the five `/api/v1` routes and the calendar feed; assets are served by the platform first, so only these paths invoke the script |
| `scripts/contract.mjs` | G1 — the same requests against `lineup serve` and `wrangler dev`, diffed by status and body/error code |
| `cmd/lineup/` | the entry point: `mine`, `verify`, `doctor`, `serve`, `publish`; `findings` still names its phase |
| `internal/model/` | `Show` (full fidelity), `Row` (the published nine keys), identity keys |
| `internal/clock/` | the only place the wall clock is read; every selection path takes the time as a parameter |
| `internal/index/` | the read side of the published format: manifest, picker, shards, the row contract |
| `internal/doctor/` | one report on the state of an install, without changing anything |
| `internal/mine/` | a sweep: towns → candidates → details → rows → merge, plus the report and the coverage block |
| `internal/match/` | the merge: `artistKey\|venueKey\|date`, with a position fallback for one room spelled two ways |
| `internal/sources/<platform>/` | one adapter per platform: `Tickster` is microdata, `dice` is JSON-LD; both expose `TownCandidates` + `Detail` |
| `internal/fetch/` | the HTTP client: browser header profile, explicit gzip, per-host limiter, robots cache |
| `internal/sources/` | one adapter per platform, plus the registry loader (P0.7+) |
| `internal/venues/` | the music policy: a room's class (music/other) and the event forms that are never a gig |
| `sources/` | the registry, data only — `countries/*.toml`, `aliases/*.toml`, and `venues/<market>.json` (the classified rooms) |
| `web/` | the board; built to `web/dist` and served as static assets |
| `wrangler.jsonc` | the one deployment: assets first, script only for `/api/v1/*` and `/calendar/*` |

## Commands

```bash
make build     # bin/lineup
make verify    # check a built index; ARGS="--index <dir>" to point it elsewhere
make mine      # sweep platforms and write an index; ARGS="--source dice,tickster --town copenhagen --interval 2s"
make mine ARGS="--source tickster --town copenhagen --dry-run --report r.json"   # measure only
make serve     # serve the built index at 127.0.0.1:8016 (--web for the board, --now to freeze the clock)
make web       # build the board into web/dist (then `make serve --web web/dist`)
make worker-dev # the Worker at 127.0.0.1:8788 (stub board until P4), then `make contract` to diff the two
make publish   # verify, then upload to the bucket; ARGS="--dry-run" to list
make doctor    # report the install: version, go, clock, index state, registry
make dev       # the local pair (lands in P3 with `lineup serve`)
make keycheck  # the identity contract: the TS twin against testdata/keys.json
make geocheck  # the distance contract: radius, formatter, and the position pairs a source actually emits
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
- **No selection path reads the clock.** The miner takes `Now` in its options and the board's selection
  functions take the time as a parameter, because every user-input scenario asserts a date window.
  `make clock-check` fails the build if the wall clock is read anywhere in Go outside `internal/clock`.
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
- **The index is music only.** A room that does not programme music is dropped, and so is an event
  form that is never a gig; the decision is `internal/venues` and its data is `sources/venues`. A room
  nobody has classified is published and counted in the coverage record — the count is the next
  curation pass, not a silent hole.
- A source that answers 0 is investigated with the full browser header profile before it is recorded
  as blocked — a TLS reset on a UA-only request is not evidence of anything.
- Delete the old layer in the same change that ships the new one. No compatibility shims.
- Update `PROJECT.yaml`, this file and the skill in the same PR as a structural change.
- `sources/` is data; community contributions touch it and nothing else.

## Where the depth is

`docs/decisions/` — the ADRs behind the index hosting, the row contract, the tier and the
publication posture. The full build plan is agent-side; this file and the ADRs are the contract that
ships.
