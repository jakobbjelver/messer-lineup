# ADR-001 — index hosting, row contract, and the tier

**Date:** 2026-11-17 · **Status:** accepted · **Supersedes:** the earlier plan's `deploy.kind: edge`,
its interned shard example, its `latest.json.gz` alias and its Pages + Worker shape.

Three irreversible choices. Each one is expensive to reverse once the row type compiles.

---

## 1. Index hosting: object storage with a custom domain, one Worker for the board and the API

**Decision.** `data.lineup.messer.wtf` is a public R2 bucket behind a custom domain; the index is
written by `lineup publish` with per-object metadata (`Cache-Control: public, max-age=31536000,
immutable` for shards, `max-age=300` for the manifest and `towns.json`). `lineup.messer.wtf` is
**one** deployment with a static-assets directory and a script: assets are served without invoking
the script, and the script answers only `/api/v1/*` and `/calendar/*.ics`.

**Why not a Pages project for the index.** A Pages project caps at **20 000 files** and **500
builds/month** on free. A daily publish of a 5 000-town world index writes 5 000 shard objects a day;
as a Pages "deploy" that is a file cap and a build quota in the same year. Object storage has no file
cap, no build concept, and free egress.

**Why this scales, in the numbers that actually bind:**

| Limit | Value | This project |
|---|---|---|
| Object storage (free tier) | 10 GB-month | world index **32 MiB** (measured size math); 0.3 % of the allowance |
| Class A writes (free tier) | 1 M/month | ~121 objects/day now, ~5 000/day at world scale → **150 k/month** |
| Class B reads (free tier) | 10 M/month | shards are immutable and edge-cached: one origin read per PoP per version, ~300 × 5 000 = **1.5 M** worst-case cold |
| Egress | free | a page view costs nothing to serve |
| Worker requests (free tier) | 100 000/day | **API only.** Asset requests are free, unmetered and never invoke the script |
| Worker CPU (free tier) | 10 ms | the API is a cache read + JSON; the ICS route is the one risk (below) |
| Worker memory | 128 MB | a town shard is ≤ 300 KB gzipped |

**Consequences, all of them required:**

- **The assets directory is served first, and nothing may override that.** A route pattern that
  always invokes the script is metered and, at the daily limit, refuses instead of falling back to the
  assets. The default — assets first, script only on a miss — is what keeps a page view free.
- **The board uses query-parameter routes, not paths** (`/?town=copenhagen-dk&reach=60`). Then every
  page view is `/` + `/assets/*`: all assets, no script. A path-routed board would send every deep
  link through the script.
- **The ICS endpoint is the only CPU-risky route.** Precompute the default spec per town at publish
  time and upload it as a static file (`/calendar/<town>.ics`); the dynamic route serves only custom
  specs and memoises its rendered output in the edge cache for an hour. If a town's feed ever exceeds
  10 ms of CPU, the answer is precomputation, not a paid plan.
- **A lifecycle rule expires content-addressed shards after 30 days.** Immutable names mean old
  objects accumulate: 300 towns × ~10 KB × 30 days ≈ 90 MB steady state. Without the rule it is
  unbounded.
- **Tiered cache on the index domain**, so one upper tier sits next to the bucket.
- **No `latest.json.gz`.** Clients read `manifest.json` (`If-None-Match`, 60 s) → `towns.json` for
  the picker → the shard named by `sha256`, `cache: immutable`. Any `latest.*` alias is an
  invalidation protocol wearing a costume.

**Deployment consequence that must not be discovered at launch.** An operator who fronts this
deployment with an access layer (an authenticating proxy, a tunnel with a blanket policy) must add an
explicit bypass for **both** hostnames — the board and the index — or a public product answers 401 to
the world while every internal check passes. Verify with a cookie-less `curl` after the first deploy;
this is the product's first public surface, and it is a deliberate posture change recorded here
rather than an accident.

**Rollback.** Roll the script back to the previous version; the index is objects, so a bad publish is
"restore the previous manifest" — shards are immutable and the old one still exists inside the
30-day window.

---

## 2. Row contract: one flat row, no interning, `k` recomputable

```json
{"k":"viagra-boys|vega-cph|2026-11-19","a":"Viagra Boys","v":"Vega","c":"copenhagen-dk",
 "d":"2026-11-19T20:00:00+01:00","st":"o","u":"https://www.billetlugen.dk/...",
 "src":["billetlugen","billetto"],"g":[55.6707,12.5503]}
```

- **Flat JSONL, one row per line, gzipped.** The measured 22.2 bytes/row is this row; interning buys
  nothing after gzip and costs a document plus two index tables.
- **`k = artistKey|venueKey|YYYY-MM-DD`, computed after venue canonicalisation.** Recomputable from the
  row, stable for the calendar `UID` and for "new since last visit".
- **`g` exists because the board shows kilometres.** Without coordinates the distance column is
  unimplementable from a thin row.
- **Two Go types, one writer.** `model.Show` (miner-side, full fidelity: price, doors, images,
  description) and `index.Row` (this allowlist). There is no conversion path that carries a price, so
  the projection is structural rather than a filter that can be forgotten. A test marshals a `Show`
  through the projector and asserts none of `p, cur, desc, img, lineUp, seats` appear.
- **"Six fields" is retired as a slogan** — the allowlist has nine keys, and the policy is *no content
  fields*, not a count.
- **Town ids are `<slug>-<cc>`** (`copenhagen-dk`, `berlin-de`, `london-gb`): global slugs collide
  (`London, ON`), and the id appears in the URL and in the merge key's venue half.

---

## 3. Tier: `edge`, with the self-host binary as a dated exception

```yaml
family: messer
tier: edge
kind: repo
deploy:
  kind: cloudflare-worker
  domain: lineup.messer.wtf
  data_domain: data.lineup.messer.wtf
exceptions:
  - { id: second-serving-runtime, reason: "…", expires: 2027-06-30 }
  - { id: edge-primitive,         reason: "…", expires: 2027-06-30 }
  - { id: no-settings-page,       reason: "…", expires: 2027-06-30 }
```

`tier` is one of six values and `deploy.kind` is one of five (`docker`, `cloudflare-worker`, `pm2`,
`store`, `none`).

**Corrected while scaffolding (P0.1):** an earlier draft declared a fourth exception,
`two-deployables`. It was wrong: a static-assets directory and an API in **one** configuration are one
deployable, not two, and the exception list is not the place to count a resource twice. What genuinely
needs an exception is the second *runtime* (the self-host binary), the edge primitive itself, and the
absent settings surface — three, each dated.

The self-host binary is a second **artifact**, not a second product: same index, same adapters, bound
by the cross-runtime contract (one fixture file, one clock-free selector).

---

## Rejected alternatives

| Option | Why not |
|---|---|
| A Pages project for the board + a Worker on another hostname | two hostnames for one product: a second certificate, a second access policy, and a CORS surface between them |
| A Pages project for the index | 20 000-file and 500-build ceilings; no immutability guarantee by object metadata |
| The storage provider's non-production subdomain (`r2.dev`) | no custom domain means no cache control, no WAF, no access exemption; documented as non-production |
| D1 for the board | a read-only published index is a file; D1 stays for sessions and taste only |
| Presigned URLs for shards | throws away the edge cache and the "cache forever" property that makes page views free |

## Amendment (2026-11-18) — the index is served at `lineup-data.messer.wtf`

This ADR names the data origin `data.lineup.messer.wtf`. That name cannot have a certificate on this zone:
Universal SSL covers `*.messer.wtf`, which is one label deep, and nothing else covers a second level (the
Advanced Certificate Manager endpoints answer `1450 not granted for this zone`, and the R2 custom domain's own
certificate stays `ssl: pending`). A name whose only possible state is a TLS handshake failure is not a
decision worth keeping.

**The index is served at `https://lineup-data.messer.wtf`.** It is the same bucket, the same object names, the
same headers; only the hostname changed, and it changed to a one-level name so that the certificate the zone can
actually issue covers it. `data.lineup.messer.wtf` was retired — removed from the bucket, the Access app and the
zone's catch-all — rather than parked as a second name for one artifact.

Everything else in this ADR is unchanged: R2 rather than Pages, content-addressed `.jsonl.gz` shards, immutable
shard caching with a short-cached manifest, a 30-day lifecycle rule, and the nine-key row. The setup steps,
including the two that have nothing to do with R2 (the Access bypass app and the catch-all allowlist), are in
`docs/publishing.md`.
