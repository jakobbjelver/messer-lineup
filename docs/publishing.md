# Publishing the index

The miner builds a directory; `lineup publish` copies it into an R2 bucket that is served at
**`https://lineup-data.messer.wtf`**. That hostname is the product's only data origin: the board (P3) fetches
the manifest and one shard, and nothing else here is reachable.

The hostname is one level deep on purpose. Universal SSL covers `*.messer.wtf` and nothing on this zone covers a
second level — the Advanced Certificate Manager endpoints answer `1450 not granted for this zone` — so ADR-001's
original `data.lineup.messer.wtf` could never hold a certificate and was retired on 2026-11-18 (ADR-001 carries
the amendment). One name, one bucket, no parked alias.

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

## The guards

`lineup publish` refuses — non-zero, **nothing uploaded** — when any of these hold:

| Condition | Why |
|---|---|
| `lineup verify` rejects the index | schema, projection allowlist, window, an empty index |
| the whole index would drop more than **25 %** | a collapse is not a quiet week |
| a town of 20+ shows would drop more than **60 % while its own sources report `ok`** | the signature of a parser that stopped matching |
| a town of 20+ shows would disappear while its sources report `ok` | the same thing, one step further |

The guards read the manifest **already in the bucket** before uploading, so they compare like with like, and they
run in a `--dry-run` too — the point is to see the refusal while looking at the numbers.

Two deliberate reductions of the plan's §5.8 list:

- **"fewer than two sources `ok` for the primary town" is gone.** ADR-003 established that no town we mine has
  two sources (DICE is DE/GB/US, Tickster is DK/NO/SE), so that rule would refuse every publish forever. The rule
  it protected — a collapse nobody noticed — is the two rows above.
- **Towns under 20 shows are not guarded.** Three shows becoming one is an ordinary week, and a guard that fires
  on it teaches people to pass `--force` out of habit.

`--force` exists for the operator who has read the numbers and decided the drop is real. **An automation must
never set it**: the nightly job does not, which is why a broken parser means a quiet night rather than a quiet
board.

**A policy change is the other kind of real drop.** Adding the music filter on 2026-11-20 took the index from 754
rows to 335 (−56 %), so the guard refused the publish — correctly. The operator reads the run report and the
coverage record (both say 453 rows went, and why), publishes once with `--force`, and the next night compares
against the new baseline. If the drop cannot be explained row by row, it is a parser break, not a policy change.

## Unattended runs

`scripts/nightly.sh` does the whole cycle and writes one ledger line:

```
make build -> lineup mine --town $LINEUP_TOWNS --report .lineup-index/sweep-<day>.json
           -> lineup publish        (guards apply)
           -> ~/Artifacts/messer-lineup/ledger.md
```

- It rebuilds the binary **first**, every night, because a stale `bin/lineup` has twice produced a run that
  measured nothing and looked fine.
- Credentials come from `~/.config/messer-lineup/env` (0600, `LINEUP_R2_*`), never from the repo.
- The full log goes to `~/Artifacts/messer-lineup/logs/nightly-<day>.log`; **exactly one line** goes to stdout,
  because the scheduler delivers stdout verbatim.
- A refusal is a normal outcome and is recorded as one: the published index stays, and the ledger says so.

**Scheduled on the host, by PM2, at `10 4 * * *`** (`pm2 start scripts/nightly.sh --interpreter bash --name
lineup-nightly --cron-restart "10 4 * * *" --no-autorestart`; `pm2 save` afterwards). Not a Hermes cron job: the
scheduler runs **inside the Hermes container**, which has no `/Users` tree — the repo, Go and rclone are all on the
host, so a container-side job fails with exit 127 before the miner starts. A cron job that cannot see its own script is
worse than no cron job, because the failure arrives as a daily notification instead of a missing line in the ledger.

`pm2 logs lineup-nightly` shows the one line per run; the run's own log and the ledger are unchanged.

## One-time setup (already done, recorded for the next person)

1. Bucket `messer-lineup-index` in the `messer.wtf` account.
2. Lifecycle rule: `towns/` → delete after 30 days.
3. Custom domain `lineup-data.messer.wtf` on the bucket (creates a proxied CNAME to `public.r2.dev`). **The
   hostname must be one level deep**: see the certificate note above.
4. **Access bypass for the hostname.** The zone's `Private Admin Apps` app covers `*.messer.wtf`, so every
   uncovered subdomain answers 401 to the world. A self-hosted app named `Lineup Index (public)` on
   `lineup-data.messer.wtf` with a `Bypass Everyone` policy is what makes the index readable; without it the
   bucket is correct and unreachable.
5. **Add the hostname to the zone's catch-all** (`~/Repos/messer-site`, `node scripts/sync-catchall.mjs`).
   Rule 2 of the zone's dynamic-redirect ruleset sends any host that is not in the known list to
   `https://messer.wtf/?from=<host>` with a 302 — which is what answers a bucket that is correctly configured,
   because redirect rules run before anything else. The host is in the script's explicit "outside the server
   repo" list (an R2 bucket has no origin on the tunnel and no `services.yaml` entry). The script's
   `CLOUDFLARE_API_TOKEN` must carry rulesets write; if it does not, the same ruleset can be PUT through the
   Cloudflare MCP session.
6. Wait for `status.ssl` to reach `active` on a custom domain — for a second-level name that may not come.
