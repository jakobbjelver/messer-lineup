# Messer Lineup

A season board for live music: the artists you actually listen to, playing rooms near a point you
choose, inside a date window — with the distance in kilometres and the ticket link on the row.

Lineup has no account. Your taste comes from a music service you already use, or from a list you
paste; the board itself is public, and a fresh visitor still gets the town board.

## How it works

One private miner walks event platforms country by country and writes a published **index** — a
manifest, a town list and one content-addressed shard per town. The index is static files, so serving
it costs nothing and there is no database in the read path. Only the *mined* projection is published:
nine keys per show, no prices, no descriptions, no images, every row linking out to the seller.

```
lineup mine   →  index/{manifest.json, towns.json, towns/<town>-<sha256>.jsonl.gz}
lineup verify →  the publication rules, checked before anything leaves the machine
lineup serve  →  the same index, locally, for self-host
```

The board is a React app served beside a small API; mining never happens there.

## Run

```bash
make build     # bin/lineup
make check     # gofmt + vet + tests + the public-tree scan
./bin/lineup version
```

Status: **P0** — the skeleton, the identity contract, the fetch profile and the first source
adapters are being built; `lineup` currently answers `version` only. See `docs/coverage.md` for the
measured state of each source once it exists.

## Layout

| Path | What |
|---|---|
| `cmd/lineup/` | the one binary: miner, publisher, local server |
| `internal/` | model (rows, identity), fetch, sources, board selection, index writer |
| `sources/` | the source registry — data only, one file per country |
| `web/` | the board (React), built to `web/dist` and served as static assets |
| `internal/model/testdata/keys.json` | the identity contract: 30 vectors both runtimes must agree on |

## Design notes

**Identity.** A show's key is `artistKey|venueKey|date`. Names are normalised the same way in Go and
in TypeScript, and the vector file is the contract — an implementation that disagrees with it is
wrong, and the vectors are never edited to make a test pass.

**Publication.** Only a thin projection is published, and a test proves a full-fidelity show cannot
leak a price or an image into it. The published window is 120 days, every row links out, and robots
and takedown requests are honoured. The index is an advertisement, never an archive.

**Two runtimes, one contract.** Serving happens on an edge network; the miner runs on the machine
that owns the crawl. Both read the same index files, and the parts that must agree are held to one
fixture file rather than to prose.

## Deploy

The index is published to object storage behind an immutable cache; the board and its API are one
deployment with static assets served first and the script reached only for API routes.

## License

MIT — including the miners and the index format. Contributions: see `CONTRIBUTING.md`.
