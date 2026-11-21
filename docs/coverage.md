# Coverage — what each source actually measures

This file is the honesty record. It is written from a measurement, not from an intention: a source is
listed as reachable only if a request from the machine that will run the miner returned its content.
The measured block below is written by `lineup mine --coverage docs/coverage.md`; the prose is hand-written.

**Measured 2026-11-17 from the Mac mini, through `internal/fetch`** (full browser header profile, cookies
carried, `Referer` on click-through, robots honoured). Reproduce with `make probe`.

Byte sizes below are one sample; a dynamic page varies a little per request, which is why every verdict is
a status code and never a size.

## v2: the second source landed (2026-11-21)

**Nortic** is the second source, and the v1 note's "dead as HTML" verdict was wrong: the city pages link to
`tickets.nortic.se/ticket/event/<id>`, and that host publishes `sitemap-events.xml` (2 226 URLs at measurement).
Every event page carries schema.org `Event` JSON-LD and a `[Konsert]` / `[Festival]` category prefix, which is a
cleaner music filter than anything Tickster publishes.

The first two-source run: **1 041 rows** (Tickster 717, Nortic 324), four towns, duplicate rate **3.3 %**,
`lineup verify` green, published. Stockholm 535, Malmö 218, Gothenburg 264, Copenhagen 24. The rooms Nortic adds are
the ones v1 had zero rows for: Plan B (34 rows), Palladium (29), Fasching (77), Kollektivet Livet (61),
Eric Ericsonhallen (12), Münchenbryggeriet (10), Storkyrkan (18), Malmö Live.

**The hit rate did not move.** 42 Last.fm artists and 17 ListenBrainz artists still match **0** of 1 041 rows. The
index is broader (913 distinct artist names) but the artists on those two lists are not touring Swedish rooms in this
window — the window is 21 Nov → 21 Mar, and the lists are classic rock and Americana, whose tours are summer. That is
a fact about the season and the inventory, not about the parser. The next levers are the artist-name field (26 % of
rows still carry a show title: `Gud förbannat - Nu får det bära eller brista`, `Möt Lucia i Storkyrkan`) and genre
input, not another platform.

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

## The board is a music board

Tickster is a town's **whole** ticketing inventory, not a concert list: theatre runs, julbord cruises, wine
tastings, museum workshops, stand-up and football screenings are sold on the same pages as the gigs. Measured on
the live index before the fix: 718 of Stockholm's rows, of which 430 were at rooms that never programme a music
event, and 12 more were non-gig forms at rooms that do.

The rule is two things, and neither is a keyword search for "konsert":

- **A room's class.** `sources/venues/<market>.json` lists every room the index has published and whether its
  programme is music or not (130 rooms: 65/65 at the first pass). A theatre's row is dropped by the room, not by
  the title, because a play at Orionteatern is not a gig whatever it is called.
- **An event form.** At a room that does programme music, a short list of forms is never a gig: `julbord`,
  `provning`, `workshop`, `standup`, `frukost`, `quiz`, `wrestling`, `vendors`. Ten rows went this way.
  A **positive** rule ("the title must contain a music word") was measured and discarded: it drops real gigs
  called *Elton John meets Freddie Mercury* and still admits *JULKONSERT* — the negative list is the honest
  shape. `teater` is deliberately **not** a token: Tickster appends the room to the title (*Billy Opel - Södra
  Teatern*) and a band is called *Nationalteaterns Rockorkester*; a `teater` token dropped nine real gigs.

A room nobody has classified is **published and counted**, not dropped: a new market must be measurable before
its rooms are curated, and the count in the block above is the curation work list. The miner's report carries the
per-room breakdown (`dropped_venues`, `unclassified_venues`), so the next pass is one JSON edit.

## The last measured run

Rewritten by `lineup mine --coverage docs/coverage.md` — or re-rendered from a saved report with
`lineup mine --from <report.json> --coverage docs/coverage.md`, which is how this block was written (the sweep
itself ran for ~11 minutes and its table should not be a second measurement). The prose around the block is
hand-written on purpose; the numbers inside it are a command's output.

<!-- mine:begin -->
_nortic, tickster · measured · 2026-11-21T18:00:13Z_

| Town | Source | Candidates | Rows | Not music | Filtered | Failures | Seconds |
|---|---|---:|---:|---:|---:|---:|---:|
| copenhagen | tickster | 48 | 24 | 11 | 0 | 0 | 34 |
| gothenburg | nortic | 2226 | 8 | 1087 | 0 | 16 | 0 |
| gothenburg | tickster | 679 | 258 | 252 | 0 | 0 | 371 |
| malmo | nortic | 2226 | 77 | 1088 | 0 | 16 | 0 |
| malmo | tickster | 263 | 157 | 54 | 0 | 0 | 146 |
| stockholm | nortic | 2226 | 239 | 1097 | 0 | 16 | 1131 |
| stockholm | tickster | 949 | 314 | 434 | 0 | 0 | 553 |
| **total** | | **8617** | **1077** | **4023** | **0** | **48** | |

Music policy: **4023** rows dropped (category 3243, form:allsang 3, form:bingo 1, form:brunch 1, form:dinner 2, form:forestallning 1, form:fotboll 2, form:frukost 1, form:humor 2, form:julbord 9, form:kidz 1, form:loppis 2, form:marknad 1, form:middag 16, form:musical 2, form:musikal 2, form:provning 36, form:pyssel 1, form:quiz 3, form:stand-up 3, form:standup 4, form:tasting 3, form:vendors 1, form:wrestling 2, venue 681).

Merged: **1077** rows → **1041** shows, duplicate rate **3.3 %**. Towns discovered: **4**.
Index: **4** shard(s), 1041 rows published, 0 outside the window, 0 refused, 57 643 bytes.

Why pages were lost: other: nortic: https://tickets.nortic.se/ticket/event/78806: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/79922: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/80512: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/80775: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/82725: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/83405: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/85664: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/85768: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/85786: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/86326: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/86411: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/86414: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/87227: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/87255: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/87279: the page carries no application/ld+json ×3; other: nortic: https://tickets.nortic.se/ticket/event/88667: the page carries no application/ld+json ×3;
<!-- mine:end -->

## The hit rate, measured after three towns

| List | Artists | Rows matched |
|---|---|---:|
| Last.fm `messeratgalten`, top artists (12 months *and* lifetime) | 42 | **0** |
| ListenBrainz `jakob1337`, top artists this year | 17 | **0** |

Four towns, 719 rows, 645 distinct artists — and two real taste lists match **nothing**. That is the coverage gap
stated as a number rather than an adjective: every room on this index arrives through Tickster, and Tickster's
inventory is the mid-size Nordic promoter layer, not the clubs and concert halls that book the artists a listener
actually plays. No list length, cap or UI change moves this; a second source per town does. Re-measure with
`scripts/inventory.py --index .lineup-index` and the same diff against a provider's top-N.

**Re-measured 2026-11-21 at 1 041 rows and two sources: still 0 and 0.** The v2 section above has the reading; the
number that matters did not move, and the record should not pretend otherwise.

## Four towns, one platform, and what the merge actually found

The run recorded above is the nightly shape: `--town copenhagen,stockholm,gothenburg,malmo` at a 2 s interval
(robots asks for `Crawl-delay: 2`): **1 961 candidates → 755 rows → 719 published**, four shards, 43 993 bytes,
duplicate rate **4.8 %**. Stockholm lists **962 events** on its city page, Gothenburg **685** and Malmö **266**;
the full walk is ~18 minutes and is the nightly's job.

**The merge found 36 duplicates (4.8 %) with only one source.** They are not cross-platform matches — every row
came from Tickster — they are the same artist, venue and date listed twice on the platform itself (a series with
separate ticket types, or a duplicated listing). That is a different and smaller claim than the plan's gate
made: a single source can prove the merge *runs* and that duplicates exist under the 8 % cap, but only a second
source covering the same town can prove the merge *finds* the same gig twice. Which is why the merge is also
asserted by fixtures (ADR-003), and why each town's published source count is one.

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

**Tickster, the source the nightly actually runs** (measured on the mini, robots delay honoured):

| Town | Events walked | Rows | Sweep |
|---|---|---|---|
| Stockholm | 962 | 315 | 527 s |
| Gothenburg | 685 | 258 | 378 s |
| Malmö | 266 | 158 | 144 s |
| Copenhagen | 48 | 24 | 27 s |

A nightly over four towns is therefore **~18 minutes**, and the cost is linear in candidates because every
candidate is one detail fetch at the publisher's delay. The town walk is capped (`maxPages = 120`, 1 920
events): the cap is a bound, not a target — a town that reaches it **fails the run** rather than publishing
half of itself.

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

## Festivals: the festival is the artist (decided 2026-11-20)

A festival ticket is **one row whose artist is the festival's own name** (`Viva Sounds`, `Hiphop Revolution
Festival`), not its line-up: the bill is a claim the platform publishes, and a multi-day pass is not a
performance at a time. The row sorts by the festival's first day, links to the festival page, and a reader who
cares about one name in the bill still finds it the night a promoter publishes that name as its own row. There is
no separate "festivals" browse — one board, one calendar, one subscription. The music policy already keeps a
festival: a room's class and an event's form decide, never the word "festival".

## The second source: a dated answer per candidate (2026-11-20)

| Candidate | What it does from this host | Verdict |
|---|---|---|
| Nortic | `tickets.nortic.se/sitemap-events.xml` lists 2 226 event URLs; every page carries `Event` JSON-LD with a `[Konsert]`/`[Festival]` category | **yes — shipped 2026-11-21** (see the v2 section) |
| Ticketmaster Discovery | the register form is a moderation queue, the key is shown in the portal and never emailed, and Denmark is not a Discovery market | **no, for now** — nothing is gated on it; revisit only if the account clears |
| Eventim SE | sitemaps 200, every event page 403 behind the same edge as `.de`; an off-mini test needs an egress we do not own | **no** |
| Billetto | `202` challenge page, including `/api/v3/*` | **no** |
| Billetlugen | listings yield links, event pages 403, cards carry no date and no venue | **links only** |
| DICE | 404 for every Nordic town (DE/GB/US only) | **no, for the Nordics** |

So v1 shipped **one source per town**, written down rather than implied. v2 added the second (Nortic, SE only); more
towns on a platform we can already read remain the cheap lever, but the table above is why the second platform came
first. The next town still needs a classification pass, not an adapter.

## What this means for the gates

- **G0-global** (Berlin, London, Austin ≥ 20 each from one keyless adapter) is **met by DICE**, measured town by
  town: 30/30/30 rows, each in the right town. Eventim DE and AXS cannot do it from this egress.
- **G0-local** is re-cut to a one-source promise (ADR-003): **Copenhagen clears ≥ 20 rows from Tickster alone** —
  51 candidates → 37 rows, 14 outside the window, zero failures, one shard of 2 375 bytes, `lineup verify`
  green. The two-source clause was unreachable in v1 (Billetto answers a challenge page, Billetlugen's event pages
  answer 403); **since 2026-11-21 Stockholm, Gothenburg and Malmö clear it from two sources**, and Copenhagen still
  clears it from Tickster alone.
- **The duplicate rate is live now.** Three towns have two sources (Tickster + Nortic), and the first two-source run
  merged 1 077 rows into 1 041 shows — **3.3 %**, under the 8 % cap. Thirty-six rows were the same gig listed twice,
  which is the cross-source match the fixture-only version could not prove. `internal/match` still holds the rules:
  same gig from two sources merges, two gigs in one room on one night do not.
- **A town costs one classification pass, not a new adapter**: `--town gothenburg` and `--town malmo` reused the
  `se` market file and the same microdata parser; the work was 69 rooms (45 music / 24 other) and 27 rooms
  (15 / 12). Malmö is small enough to read in one page (266 candidates, 144 s) and its duplicate rate is 9.2 %
  before the merge — Tickster lists the same gig twice more often there than in Stockholm. Oslo is the same job
  again and needs a `no.json` first.

## Sources that are not a gate

Ticketmaster's Discovery key is optional and its account sits in manual moderation; nothing in this
product depends on it. It is listed here only so the absence is on the record rather than implied.
