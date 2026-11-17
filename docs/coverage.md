# Coverage — what each source actually measures

This file is the honesty record. It is written from a measurement, not from an intention: a source is
listed as reachable only if a request from the machine that will run the miner returned its content.
The measured block below is written by `lineup mine --coverage docs/coverage.md`; the prose is hand-written.

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
| Billetlugen (`www.billetlugen.dk`) | DK | sitemap index 200 (258 B, 9 children); city/category listings 200 | **403** on every event page (431 B) — same edge as Eventim, whose `DK-eventim` assets it serves | **Links only.** Its listings render 16–22 event links with **no date and no venue** in the HTML (the cards hydrate client-side), so it cannot produce a row. |
| Tickster (`www.tickster.com`) | SE, NO, DK | **200** — 2 320 311 B, 11 325 URLs (`/se/sv` 10 313, `/no/nb` 648, **`/dk/da` 314**), date in every event URL; city pages `/dk/da/events/in/<city>?skip=N` at 16 a page | **200**, ~20 KB pages, no JSON-LD — but **schema.org microdata** (`itemprop="startDate"`, `location` with a `PostalAddress`) | **Copenhagen's only source, and now an adapter.** 51 candidates → 37 rows in one run. |
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

DICE has **no Nordic town at all**: `/browse/copenhagen`, `/browse/stockholm`, `/browse/oslo`, `/browse/malmö`
and `/browse/gothenburg` all answer **404**. DICE covers Berlin, London and the US; Tickster covers DK, NO and SE.
The two town lists do not intersect — which is why no town we can mine has two sources, and why the duplicate
rate is asserted by fixtures rather than measured live (ADR-003).

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

Rewritten by `lineup mine --coverage docs/coverage.md` — or re-rendered from a saved report with
`lineup mine --from <report.json> --coverage docs/coverage.md`, which is how this block was written (the sweep
itself ran for ~11 minutes and its table should not be a second measurement). The prose around the block is
hand-written on purpose; the numbers inside it are a command's output.

<!-- mine:begin -->
_tickster · measured · 2026-11-17T22:53:42Z_

| Town | Source | Candidates | Rows | Filtered | Failures | Seconds |
|---|---|---:|---:|---:|---:|---:|
| copenhagen | tickster | 51 | 37 | 0 | 0 | 108 |
| stockholm | tickster | 200 | 200 | 0 | 0 | 521 |
| **total** | | **251** | **237** | **0** | **0** | |

Merged: **237** rows → **226** shows, duplicate rate **4.6 %**. Towns discovered: **2**.
Index: **2** shard(s), 226 rows published, 0 outside the window, 0 refused, 13 423 bytes.
<!-- mine:end -->

## Two towns, one platform, and what the merge actually found

The run above is `--town copenhagen,stockholm --max-per-town 200` at a 2 s interval (robots asks for
`Crawl-delay: 2`): 251 candidates, 237 rows, 226 published, two shards, 13 423 bytes. Stockholm lists **928
events** on its city page (58 pages of 16); the 200 fetched here are a sample, and the town's full sweep is a
nightly job rather than a test.

**The merge found 11 duplicates (4.6 %) with only one source.** They are not cross-platform matches — every row
came from Tickster — they are the same artist, venue and date listed twice on the platform itself (a series with
separate ticket types, or a duplicated listing). That is a different and smaller claim than the plan's gate
made: a single source can prove the merge *runs* and that duplicates exist under the 8 % cap, but only a second
source covering the same town can prove the merge *finds* the same gig twice. Which is why the merge is also
asserted by fixtures (ADR-003), and why Copenhagen's published source count is one.

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
- **G0-local** is re-cut to a one-source promise (ADR-003): **Copenhagen clears ≥ 20 rows from Tickster alone** —
  51 candidates → 37 rows, 14 outside the window, zero failures, one shard of 2 375 bytes, `lineup verify`
  green. The two-source clause is unreachable: Billetto answers a challenge page, and Billetlugen's event pages
  answer 403.
- **The duplicate rate is a fixture, not a live number.** No town we mine has two sources (DICE: DE/GB/US;
  Tickster: DK/NO/SE), so `internal/match` proves the merge — same gig from two sources merges, two gigs in one
  room on one night do not, a venue spelling difference merges only on position — and `lineup mine --source a,b`
  will report a live rate the day two sources share a town.
- **Stockholm is next and cheap**: Tickster `/se/sv` is 10 313 URLs and the adapter already reads `/se/` pages
  (town, zone and locale come from the URL). Eventim SE stays index-only, sharing its `.de` sibling's verdict.

## Sources that are not a gate

Ticketmaster's Discovery key is optional and its account sits in manual moderation; nothing in this
product depends on it. It is listed here only so the absence is on the record rather than implied.
