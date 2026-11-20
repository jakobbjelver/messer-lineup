# Serving: what runs where, and why a page view is free

Two hosts, two jobs. They never overlap, and that separation is the whole cost model.

| Host | What it is | What it answers | Cost |
|---|---|---|---|
| `lineup-data.messer.wtf` | R2 bucket `messer-lineup-index` | `manifest.json`, `towns.json`, `towns/<sha>.jsonl.gz`, `artists/portraits.json` | R2 reads + egress; no compute |
| `lineup.messer.wtf` | one Worker: static assets + a small script | the built board, `/healthz`, `/readyz`, `/api/v1/artists/*`, `/calendar/*.ics` | asset requests free and unmetered; only the script is metered |

**The index is not proxied.** The board fetches the manifest and one shard straight from the bucket, so a
shard never travels through a Worker and never costs CPU. The Worker's only read of the bucket is `/readyz`'s
one manifest fetch, on demand.

**Assets are served first, and the script is never forced.** `wrangler.jsonc` must not grow a
`run_worker_first` (or `assets.run_worker_first: true`): with it, every request — including a page view —
reaches the script and is metered, and the free plan answers 429 at the daily limit instead of falling back.
Without it, a page view is an asset request: free, unmetered, and not counted against CPU. `/healthz`,
`/readyz`, `/api/v1/*` and `/calendar/*` are not files, so they are the only paths that invoke the script.

## The route table

Every route below has a consumer. There is no account, no session and no taste provider beyond Last.fm, so
there is no `/auth/*`, no `/session`, no `/taste/me` and no `/taste/curate` — a path this build does not serve
answers the contract's 501 envelope, which is an honest "not mine".

| Route | Answer | Read by |
|---|---|---|
| `GET /healthz` | 200 `{ok, as_of}` | the deploy check, the dev server's readiness probe |
| `GET /readyz` | 200 when the bucket's `manifest.json` is reachable, 503 with a `detail` when it is not | uptime |
| `GET /api/v1/artists/search?q=` | MusicBrainz search, serialised and cached at the edge; `unavailable` instead of an empty lie | the artist picker's combobox |
| `GET /api/v1/artists/portraits?n=…` | up to 8 names → Deezer faces, keyed by the name that was sent | the picker's avatars |
| `GET /calendar/<token>.ics` | the feed for a board (town or `anywhere`, optional taste snapshot or live ListenBrainz source) | a calendar client's subscription |
| everything else | the assets, or the SPA document | the board |

`/towns.json`, `/manifest.json` and `/towns/<sha>` are served by `lineup serve` from a local index and by R2 in
production — same bytes, same headers, and the board does not care which host it is talking to.

## The board

The SPA is `web/` (Vite + React). It fetches **two** things, both from the index host: `manifest.json`, then the
one shard for the scope it is showing (`anywhere` is the union of every shard, deduped by row key). Nothing is
proxied through the Worker, and the shard's name is its own digest, so the browser cache holds it forever while
the manifest — five minutes — is the only thing that moves.

That cross-origin fetch is why the bucket carries a CORS policy:

```
GET/HEAD, origins https://lineup.messer.wtf and http://localhost:5173
expose content-length, content-type, content-encoding, etag · max-age 86400
```

It is narrow on purpose: a request from any other origin gets no `access-control-allow-origin` header, which is
what the board's own error state renders when the bucket is misconfigured — a real failure that says so instead
of a blank page.

Two builds, one difference:

```
make web                                         # VITE_DATA_BASE unset → same origin → for `lineup serve`
make web-edge                                    # VITE_DATA_BASE=https://lineup-data.messer.wtf → what ships
```

`npm run dev` proxies `/manifest.json`, `/towns.json`, `/towns`, `/artists` and `/calendar` to
`lineup serve` on 8016, so HMR works against a local index and localhost never needs a CORS exemption on the
bucket.

## One contract, two runtimes

`lineup serve` (Go) and the Worker answer the same routes with the same statuses and the same envelope.
`make contract` sends the same requests to both and diffs them:

```
make serve          # terminal 1: Go, 127.0.0.1:8016, with `--web web/dist` so /a/<key> has a document
make worker-dev     # terminal 2: the Worker, 127.0.0.1:8788
make contract       # diffs the two — 11 cases, all agreeing
```

What is compared depends on the route: a deterministic route must match byte for byte after parsing
(`kind: "body"`); a route carrying a clock must match on the listed keys (`kind: "shape"`); a document route
must answer `text/html` with `#root` (`kind: "document"`); a feed must be structurally valid ICS with unique
UIDs (`kind: "ics"`); a path that is not a route must match status and `error.code` (`kind: "error"`).

## Deploying

```
npm install
npm run check                                     # tsc --noEmit for the Worker
make web-edge                                     # the assets, built against the bucket
npx wrangler deploy                               # the assets ARE the board
```

**Deploy the edge build, not the same-origin one.** They differ only in `VITE_DATA_BASE`, and the difference is
fatal to the wrong choice: a same-origin build asks the Worker for `/manifest.json`, which is neither a file nor
a route, so the board renders its error state. `make web-edge` is the one that goes up.

**Three things must be true before the host answers, and two of them are not in this repo.** All three failed in
turn on the first deploy, each with a different symptom:

| Symptom | Cause | Fix |
|---|---|---|
| `302` to `https://messer.wtf/?from=lineup.messer.wtf` | the zone's catch-all does not know the host | add it to `~/Repos/messer-site` `scripts/sync-catchall.mjs` (and the live ruleset) |
| `401 {"error":"invalid_token"}` | the `Private Admin Apps` Access app covers `*.messer.wtf` | a self-hosted Access app for `lineup.messer.wtf/*` with a `Bypass Everyone` policy |
| the board renders "The index could not be read" | a same-origin build was deployed | `make web-edge` |

The first two are the same pair the index needed — see `publishing.md` — because the estate's default posture is
private and this product is public. The publish order for the index is separate: shards first,
`manifest.json` last.

## Verified, and not

- **Assets-first in production is observed, not counted.** A page GET returns the document while `/api/v1/*`
  reaches the script, but the claim that a page view costs *zero* Worker invocations needs the analytics counter
  read before and after one page view.
- **Both runtimes are held to 11 contract cases**, run by hand (`make contract`); CI runs the Go gate and the
  board build, not the two-runtime diff.
