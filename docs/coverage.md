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

## What a DICE sweep costs, measured

300 candidates from `sitemap1.xml`, one detail fetch each, 1 s apart:

| | |
|---|---|
| rows built | **295 of 300** (98.3 %); 5 pages did not yield a placeable event |
| wall time | 313 s — about **1.04 s per candidate** including the fetch |
| distinct towns | 56 |
| distribution | london-gb **100**, chelmsford-gb 55, paris-fr 16, manchester-gb 12, southampton-gb 9, brighton-gb 8, nottingham-gb 7, bristol-gb 5, madrid-es 5, liverpool-gb 4, cardiff-gb 3, birmingham-gb 3, glasgow-gb 3, bordeaux-fr 3, worthing-gb 3 |
| the gate's towns in that shard | london-gb **100**, berlin-de **2**, austin-us **0**, new-york-us 2 |

Two things that follow, and one that does not:

- **Parsing is not the risk.** 98.3 % of pages produce a complete row, well past the 95 % the plan asks for.
- **`sitemap1.xml` is almost entirely the UK.** A sample from one shard cannot answer a three-town gate; the
  per-town numbers have to come from across the index, which is why the sweep samples every shard.
- **A full sweep is an overnight job.** Seven shards of ~3 500 candidates each is ~24 000 detail fetches, so
  roughly **7 hours at 1 s per request**. With a rolling 120-day window a partial sweep is usable — shards can
  rotate across nights — but a "mine the world in ten minutes" expectation is not available from this design,
  and no amount of parallelism is polite.

## What this means for the gates

- **G0-global** (Berlin, London, Austin ≥ 20 each from one keyless adapter) is reachable through DICE, which
  covers all three towns. Eventim DE and AXS cannot do it from this egress.
- **G0** (Copenhagen: two keyless sources ≥ 20 each, merged ≥ 150, duplicates < 8 %) loses its second source,
  because Billetto answers a challenge page. Billetlugen alone is one source; a second DK source has to be
  found (Tickster DK, or DICE if it ever opens Copenhagen) or the gate has to become a one-source promise
  and say so.
- **Stockholm** is unmeasured: Eventim SE shares its `.de` sibling's verdict until someone tries an `ESE`
  event page, and Tickster has not been probed yet.

## Sources that are not a gate

Ticketmaster's Discovery key is optional and its account sits in manual moderation; nothing in this
product depends on it. It is listed here only so the absence is on the record rather than implied.
