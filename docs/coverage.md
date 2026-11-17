# Coverage — what each source actually measures

This file is the honesty record. It is written from a measurement, not from an intention: a source is
listed as reachable only if a request from the machine that will run the miner returned its content.
Regenerate it from `lineup mine --report` once the miner writes one (P0.13).

**Measured 2026-11-17 from the Mac mini, through `internal/fetch`** (full browser header profile, cookies
carried, `Referer` on click-through, robots honoured). Reproduce with `make probe`.

Byte sizes below are one sample; a dynamic page varies a little per request, which is why every verdict is
a status code and never a size.

## Reachability, which is the first thing to measure

| Source | Market | Sitemap | Detail page | Verdict |
|---|---|---|---|---|
| Eventim (`www.eventim.de`, code `EVE`) | DE | **200** — index 5 100 B, 30 children; `events1…18`; shard 1 = 10 000 URLs, 188 071 B gzipped | **403** on 5 of 5 event pages (609–682 B, `Access Denied`, `errors.edgesuite.net`) | **Index only.** No date is reachable, so no row can be built. |
| AXS (`www.axs.com`) | UK, US, DE, FR, SE, AU, NZ, JP, SG | **200** — index 10 103 B, 81 children; `sitemap_event_uk` 11 784 043 B, 50 000 URLs | **403** on 3 of 3 event pages (≈107 KB bot wall) | **Index only.** |
| DICE (`dice.fm`) | UK, US, DE, FR, SE, AU, NL… | **200** — index 1 100 B, 7 shards; `sitemap1.xml` 4 999 082 B, 3 480 URLs of which **3 471 are `/event/`** | **200**, 369–443 KB; four `application/ld+json` blocks, the first a full `MusicEvent` | **Usable spine.** Town, venue, start, geo, price and line-up are all on the page. |
| Billetlugen (`www.billetlugen.dk`) | DK | n/a (city pages, no sitemap) | **200**, 308 510 B of HTML | **Reachable**; adapter is P1's work and it is bespoke (no JSON-LD). |
| Tickster (`www.tickster.com`) | SE, NO, DK | **200** — 2 320 311 B, 11 325 URLs (`/se/sv` 10 313, `/no/nb` 648, **`/dk/da` 314**) | **200**, ~20 KB pages, **no JSON-LD**; the date is in the URL (`/dk/da/events/<id>/2026-11-20/<slug>`) | Reachable. DK volume is small and several of those 313 are gift cards and subscriptions, so it is a candidate for Copenhagen's second source with a bespoke HTML adapter, not a spine. |
| Eventfrog (`www.eventfrog.de`) | DE, CH | city pages | **200**, 176 050 B, 71 event links, no JSON-LD | Reachable, but every field would come from parsing markup. |
| Billetto (`billetto.dk`, `billetto.co.uk`) | DK, UK | — | **202**, 2 040 B challenge page, including `/api/v3/*` | Not reachable as a plain client. |
| Resident Advisor (`ra.co`) | UK | — | **403**, 768 B | Not reachable from here. |

Not one of these was judged on a `curl` result: `curl` and the Go client disagree on the same host within
the same minute (TLS fingerprint), so a block seen by one is not a block seen by the other.

## DICE is the spine, and what that costs

DICE's `robots.txt` is `User-agent: *` → `Allow: /` with `Content-Signal: search=yes,ai-train=no,
use=reference`; the named AI-training crawlers are the only ones refused. An index that carries dates and
links out to the seller is a search/reference use, which is what those signals permit. Its sitemaps are
listed in the file (`/sitemaps/sitemap{1..7}.xml`), so the shard list is the publisher's own statement.

The cost is structural: **a DICE event URL is `/event/<id>` — no date and no town.** Every candidate is one
detail fetch, which is why mining is country-first and why the sweep is the expensive part of a run.

DICE has no Copenhagen listing (`/browse/copenhagen` → 404), so it does not replace Billetlugen for DK, and
it cannot stand in for Billetto's half of the Copenhagen gate.

## G0-global, measured town by town (2026-11-17)

DICE's city page is the platform's own town list, which is what §5.7 means by deriving towns from a
platform. One request per town for the candidate list, then one detail fetch each:

| Town | Candidates | Rows in that town | Elsewhere | Failures | ≥ 20 |
|---|---|---|---|---|---|
| Berlin | 30 | **30 berlin-de** | 0 | 0 | **yes** |
| London | 30 | **30 london-gb** | 0 | 0 | **yes** |
| Austin | 30 | **30 austin-us** | 0 | 0 | **yes** |
| New York | 30 | 29 new-york-us | 0 | 1 | yes |

**G0-global is met**: one keyless adapter, Berlin / London / Austin each ≥ 20, from this egress. A city page
is a page of the list rather than the whole city, so 30 is a floor and never a total.

## The last measured run

Rewritten by `lineup mine --coverage docs/coverage.md`. The prose around it is hand-written on purpose; the
block below is a command's output, so the numbers cannot drift from what was actually run.

<!-- mine:begin -->
_dice · dry run — measured, nothing written · 2026-11-17T22:21:48Z_

| Town | Candidates | Rows | Filtered | Failures | Seconds |
|---|---:|---:|---:|---:|---:|
| austin | 30 | 30 | 0 | 0 | 31 |
| berlin | 30 | 30 | 0 | 0 | 31 |
| london | 30 | 30 | 0 | 0 | 32 |
| **total** | **90** | **90** | **0** | **0** | |

Towns discovered: **3**.
<!-- mine:end -->

## What a sweep costs, measured

| | |
|---|---|
| per candidate | **≈ 1.03 s** (one detail fetch, 1 s apart) |
| one shard | `sitemap1.xml`, 3 471 candidates — a full shard is ~1 hour |
| whole index | 7 shards, ~24 000 candidates — **≈ 7 hours**, an overnight job |
| distinct towns in one 300-candidate sample of one shard | 56, dominated by London (100) — a single shard is not the world |

With a rolling 120-day window a partial sweep is usable and shards can rotate across nights. There is no
version of this that mines the world in ten minutes, and speeding it up by hitting the host harder is not a
trade worth making.

## The parser lesson this record exists for

The first wide sample produced 656 rows from 720 candidates (91 %) and looked like a source limitation. It was
not: those pages carry `"@type": "Event"` rather than `MusicEvent`, and `offers` as a **list** rather than an
object, so a strict Go struct refused a block that had every field the row needed. The reader now walks the
document by key and type-switches each value; re-measured after the change, **80 of 80 pages parse**. The
lesson is in the adapter's own comments: JSON-LD is a shape, not a schema.

## Towns are discovered, not declared

`towns.json` is built from the shows a run produced (`internal/index.TownSet`): a town appears the first time
a mined show belongs to one, its position is the mean of the venues seen in it, and its `tz` is left empty
because no event page carries a trustworthy zone name. That is §5.7's "towns are derived from each platform's
city list" applied to the data, and it means a country can be added without a hand-written town file.

## What this means for the gates

- **G0-global** (Berlin, London, Austin ≥ 20 each from one keyless adapter) is **met by DICE**, measured town by
  town: 30/30/30 rows, each in the right town. Eventim DE and AXS cannot do it from this egress.
- **G0** (Copenhagen: two keyless sources ≥ 20 each, merged ≥ 150, duplicates < 8 %) loses its second source,
  because Billetto answers a challenge page. Billetlugen alone is one source. Tickster DK is reachable (314
  URLs, date in the URL, no structured data) and is the only candidate found, so the second source is a
  bespoke HTML adapter — or the gate becomes a one-source promise and says so.
- **Stockholm** is unmeasured: Eventim SE shares its `.de` sibling's verdict until someone tries an `ESE` event
  page, and Tickster SE / `/se/sv` is the other candidate (10 313 URLs, date in the URL).

## Sources that are not a gate

Ticketmaster's Discovery key is optional and its account sits in manual moderation; nothing in this
product depends on it. It is listed here only so the absence is on the record rather than implied.
