# Serving: what runs where, and why a page view is free

Two hosts, two jobs. They never overlap, and that separation is the whole cost model.

| Host | What it is | What it answers | Cost |
|---|---|---|---|
| `lineup-data.messer.wtf` | R2 bucket `messer-lineup-index` | `manifest.json`, `towns.json`, `towns/<sha>.jsonl.gz` | R2 reads + egress; no compute |
| `lineup.messer.wtf` | one Worker: static assets + a small script | the built board, `/healthz`, `/readyz`, `/api/v1/*`, `/calendar/*.ics` | asset requests free and unmetered; only the script is metered |

**The index is not proxied.** The board fetches the manifest and one shard straight from the bucket, so a
shard never travels through a Worker and never costs CPU. The Worker's only reads of the bucket are
`/readyz`'s one manifest fetch, on demand.

**Assets are served first, and the script is never forced.** `wrangler.jsonc` must not grow a
`run_worker_first` (or `assets.run_worker_first: true`): with it, every request — including a page view —
reaches the script and is metered, and the free plan answers 429 at the daily limit instead of falling back.
Without it, a page view is an asset request: free, unmetered, and not counted against CPU. `/healthz`,
`/readyz`, `/api/v1/*` and `/calendar/*` are not files, so they are the only paths that invoke the script.

## The route table

| Route | Today | Later |
|---|---|---|
| `GET /healthz` | 200 `{ok, as_of}` | unchanged |
| `GET /readyz` | 200 when the bucket's `manifest.json` is reachable, 503 with a `detail` when it is not | unchanged |
| `GET /api/v1/taste/handle` | 501 `not_implemented` | Last.fm / ListenBrainz handle → artist keys (P5) |
| `GET /api/v1/auth/<provider>/start` · `/callback` | 501 | Spotify PKCE, Google OAuth (P5) |
| `GET /api/v1/taste/me` | 501 | linked accounts → keys (P5) |
| `POST /api/v1/taste/curate` | 501 | the post-import drop list (P5) |
| `DELETE /api/v1/session` | 501 | forget everything (P5) |
| `GET /calendar/<spec>.ics` | 501 | the feed (P5) |
| everything else | the assets, or the SPA document | unchanged |

The 501s are honest answers in the contract's envelope — `{"error":{"code":"not_implemented","message":…}}`
— so a client can tell "not mine" from "not yet", and so the envelope is under test before the routes exist.
`message` is free text and names its runtime; `code` and the status are the contract.

## The board

The SPA is `web/` (Vite + React). It fetches **two** things, both from the bucket: `manifest.json`, then the one
shard for the town it is showing. Nothing is proxied through the Worker, and the shard's name is its own digest,
so the browser cache holds it forever while the manifest — five minutes — is the only thing that moves.

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
VITE_DATA_BASE=https://lineup-data.messer.wtf npm run build   # the edge build: assets on the Worker, data on R2
```

`npm run dev` proxies `/manifest.json`, `/towns.json` and `/towns` to `lineup serve` on 8016, so HMR works
against a local index and localhost never needs a CORS exemption on the bucket.

## One contract, two runtimes

`lineup serve` (Go) and the Worker answer the same six routes with the same statuses and the same envelope.
`make contract` sends the same requests to both and diffs them:

```
make serve          # terminal 1: Go, 127.0.0.1:8016, against .lineup-index
make worker-dev     # terminal 2: the Worker, 127.0.0.1:8788, against a stub board until P4
make contract       # diffs the two
```

What is compared depends on the route: a deterministic route must match byte for byte after parsing
(`kind: "body"`); a route carrying a clock must match on the listed keys (`kind: "shape"`); a stub must match
on status and `error.code` (`kind: "error"`). When a route becomes real, its entry flips to `body` — the
assertion grows with the implementation instead of being rewritten around it.

## Deploying

```
npm install
make worker-check                                 # tsc --noEmit
make web-edge                                     # assets built with VITE_DATA_BASE at the bucket
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

## What is not verified yet

- **Assets-first in production.** It is *observed* (a page GET returns the document, and the script's own routes
  answer separately) but not *counted*: the claim that a page view costs zero Worker invocations needs the
  analytics counter read before and after one page view.
- **Assets-first in production.** Locally, `/` returns the asset and `/api/v1/*` returns the script, which is
  the right split — but the claim that a page view costs *zero* Worker invocations is only provable from the
  Worker's analytics after a deploy: one page GET, then zero new invocations on the counter.
- **`lineup.messer.wtf` today** answers the zone's catch-all redirect (302 to the front door). It becomes the
  board when the Worker is deployed with assets.
