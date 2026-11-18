# Publishing the index

The miner builds a directory; `lineup publish` copies it into an R2 bucket that is served at
**`https://lineup-data.messer.wtf`**. That hostname is the product's only data origin: the board (P3) fetches
the manifest and one shard, and nothing else here is reachable.

Both `lineup-data.messer.wtf` and `data.lineup.messer.wtf` are attached to the bucket. The second is the name
ADR-001 gives it, and it is the one to use once it has a certificate; today only the first does. Universal SSL
covers `*.messer.wtf`, which is one label deep, and nothing on this zone covers a second level: the Advanced
Certificate Manager endpoints answer `1450 not granted for this zone`, and the R2 custom domain's own
certificate has been `ssl: pending` since it was created. See ADR-001's operational note.

## What is in the bucket

| Key | Content | `Cache-Control` |
|---|---|---|
| `manifest.json` | the build: format, window, per-source health, the shard map | `public, max-age=300` |
| `towns.json` | the picker's towns (id, name, country, position) | `public, max-age=300` |
| `towns/<sha256>.jsonl.gz` | one town's published rows, gzipped JSONL | `public, max-age=31536000, immutable` |

A shard's name **is** the SHA-256 of the bytes served, which is what makes a year-long immutable cache honest: a
rebuild that changes the data writes a *different* object, and the manifest is the only file that changes under a
fixed name. The uploader sets `Cache-Control` per object; R2 stores it as object metadata and does not add one.

## The rules the publisher follows

- **Verify first.** `lineup publish` runs the same verifier as `lineup verify` and refuses to upload an index it
  rejects — a contract violation must not become a public URL.
- **Never rewrite a shard.** An existing `towns/<sha>.jsonl.gz` is skipped (HEAD first). The two mutable files
  are always uploaded; that is what a rebuild is for.
- **Never delete.** Rollback is re-uploading an older `manifest.json`, whose shards are still in the bucket
  (see the lifecycle rule) — not removing objects.
- **Refuse a broken shard.** A file that is not valid gzip of JSONL stops the publish: a truncated object would
  be a 200 with nothing behind it.
- **Nothing else is published.** The index directory is the whole input; the miner's state
  (`.lineup-state/`, the reports) is never read. Price, description, image and line-up are structurally absent
  from a row (ADR-001 §2), so nothing here can leak them.

## Lifecycle

`towns/` expires after **30 days** (rule `expire-shards` on prefix `towns/`). Content addressing means every
rebuild strands the shard it replaced; without expiry the bucket grows with every sweep. Thirty days keeps a
rollback window that is far wider than a client's cache lifetime. The two mutable files are not covered by the
rule and are overwritten in place.

## How to publish

```bash
export LINEUP_R2_ENDPOINT="https://<account-id>.r2.cloudflarestorage.com"
export LINEUP_R2_ACCESS_KEY_ID=…          # an R2 API token with object read+write on the bucket
export LINEUP_R2_SECRET_ACCESS_KEY=…      # never in the repo, never in a shell history you keep
lineup publish --dry-run                  # what would be uploaded, with the cache headers
lineup publish                            # verify, then upload
```

`LINEUP_R2_BUCKET` defaults to `messer-lineup-index`.

## One-time setup (already done, recorded for the next person)

1. Bucket `messer-lineup-index` in the `messer.wtf` account.
2. Lifecycle rule: `towns/` → delete after 30 days.
3. Custom domains `lineup-data.messer.wtf` and `data.lineup.messer.wtf` on the bucket (each creates a proxied
   CNAME to `public.r2.dev`). **A one-level host is the one that works**: see the certificate note above.
4. **Access bypass for the hostname.** The zone's `Private Admin Apps` app covers `*.messer.wtf`, so every
   uncovered subdomain answers 401 to the world. A self-hosted app named `Lineup Index (public)` on
   `data.lineup.messer.wtf` with a `Bypass Everyone` policy is what makes the index readable; without it the
   bucket is correct and unreachable.
5. **Add both hostnames to the zone's catch-all** (`~/Repos/messer-site`, `node scripts/sync-catchall.mjs`).
   Rule 2 of the zone's dynamic-redirect ruleset sends any host that is not in the known list to
   `https://messer.wtf/?from=<host>` with a 302 — which is what answers a bucket that is correctly configured,
   because redirect rules run before anything else. The hosts are in the script's explicit "outside the server
   repo" list (an R2 bucket has no origin on the tunnel and no `services.yaml` entry). The script's
   `CLOUDFLARE_API_TOKEN` must carry rulesets write; if it does not, the same ruleset can be PUT through the
   Cloudflare MCP session.
6. Wait for `status.ssl` to reach `active` on a custom domain — for a second-level name that may not come.
