# ADR-003 — Sources: Tickster is Copenhagen, and the gate is re-cut to match

- **Status:** accepted, 2026-11-17
- **Supersedes:** the source plan in the implementation plan §P1 as it applies to Copenhagen, and the
  "two keyless sources ≥ 20" clause of G0-local. It does not touch ADR-001 (hosting, row contract, tier).
- **Decision owner:** the miner (this repository). No external dependency changes.

## 1. Context — what was believed, and what the machine answered

The plan's P1 gate (G0-local) asked for **two** keyless Copenhagen sources with ≥ 20 shows each, a merged set of
≥ 150, a duplicate rate under 8 %, and a shard under 300 KB. It named Billetlugen and Billetto as the pair, with
Ticketmaster as the licensed escape hatch.

Measured on the mini on 2026-11-17, with the full browser header profile, cookies carried and a `Referer` set:

| Source | Listing | Detail page | Verdict |
|---|---|---|---|
| **Tickster** (`tickster.com`) | 200, `/dk/da/events/in/københavn` pages at 16 events a page via `?skip=` | **200, microdata** (`itemprop="startDate"`, `location`/`PostalAddress`) | **usable** |
| Billetlugen (`billetlugen.dk`) | 200, city + category listing (22 music links) | **403 on every event page** (431 B) | listing gives links only; the cards carry **no date and no venue** in the HTML, so it cannot produce a row |
| Billetto (`billetto.dk`, `.co.uk`, `/api/v3/*`) | 202 challenge | 202 | closed |
| DICE (`dice.fm`) | `/browse/copenhagen` **404**; `/browse/stockholm`, `oslo`, `malmö`, `gothenburg` **404** | 200, JSON-LD | no Nordic town at all |
| Eventim | no Danish storefront (`eventim.dk` is a CNAME to `status.eventim.de`) | 403 | not a Copenhagen source under any circumstance |

Two facts follow, and they are the whole decision:

1. **Copenhagen has exactly one reachable source: Tickster.** The second-source clause of G0-local cannot be
   satisfied by any combination of the platforms in the registry, and no licensed fallback exists (Ticketmaster
   is in manual moderation and is not a Danish Discovery market anyway).
2. **No town we can mine has two sources.** DICE covers Berlin, London and the US; Tickster covers DK, NO and SE;
   their town lists do not intersect. The duplicate rate — the one number G0 was designed to grade — is
   therefore **unmeasurable live** today, at any town, from this machine.

## 2. Decision

**a. Build the Tickster adapter.** It is the only way Copenhagen exists at all, it also opens Stockholm, Oslo,
Bergen, Aarhus, Odense and Aalborg, and it is a small, boring parser: microdata attributes, not a JavaScript app.

**b. Copenhagen ships with one source, and the coverage line says so.** A source count is published per town
(`model.Index.Sources`, rendered in `docs/coverage.md`); a town with one source states one. Nothing in the board
or the copy implies a second opinion that does not exist.

**c. G0-local is re-cut:**

| Was | Becomes |
|---|---|
| two keyless sources ≥ 20 Copenhagen shows | **one** source (Tickster) ≥ 20 Copenhagen shows in the window |
| merged ≥ 150 | merged = raw − duplicates, reported; no absolute floor invented for a single source |
| duplicates < 8 % | **measured where it can be, asserted where it cannot**: a one-source run still reports a duplicate rate — the 2026-11-17 run found 4.6 % (11 shows listed twice on Tickster itself) — but a *cross-source* duplicate needs two sources over one town, so the capability is also held by `internal/match` fixtures |
| one shard < 300 KB gzipped | unchanged — a cap, not a test |

**d. A district resolves to its city.** Tickster states `København V`, `København Ø`, `Frederiksberg` where DICE
states a city. The town id is the metro area (`copenhagen-dk`), because two ids split one city into two shards
and halve every count. Spelling disagreements are handled by a small alias table in the adapter
(`København → copenhagen`) until the registry (`sources/aliases/*.toml`, P2) replaces it; any other town gets a
deterministic transliterated id (`Herning → herning-dk`).

**e. A platform's town page is not a town.** Tickster's Copenhagen list also returns Herning and Vemb, 200 km
away. A run counts those as `other_town` and publishes none of them; the sweep is scoped by the show's own town,
not by which list nominated it.

**f. Tickster rows carry no position.** The page states a venue name and a locality, never coordinates, so `g` is
absent and the board shows the venue without a distance. That is what the row contract already allows, and it is
the honest alternative to a geocode the miner has no offline source for. The fix, when it comes, is a venue
coordinate pass (one fetch per venue, cached) or the town registry — not a guess.

## 3. Consequences

- **The merge is proven by fixtures, not by production.** `internal/match` holds the tests the plan wanted G0 to
  provide, and they run on every `make check`. When a second source does cover a town we mine, the live duplicate
  rate becomes meaningful without a code change: `lineup mine --source a,b --town x` already reports it.
- **Copenhagen's coverage is thin and its gate is small.** 51 candidates on the city list, of which the window and
  the town filter keep a subset. The town ships because it clears ≥ 20, and the number is published beside it.
- **Tickster's inventory is not all music, and nothing is filtered at ingest.** The Copenhagen list carries
  pancake workshops (`Æbleskiveværksted kl. 10.00`), wine tastings (`Vin- & Slagmarker …`), a football match
  (`Fremad Amager - Skive IK`) and wrestling alongside real concerts, and the platform exposes no category.
  Everything ticketed is published, because: the row contract deliberately has no category field, so an ingest
  filter is irreversible — a wrongly dropped show is invisible forever, while a wrongly kept one is visible and
  correctable; and every keyword rule breaks on real music (`Vinkoncert med R E M I S D U O` is a concert, `MOR`
  is a band, `A Good Year // Pumpehuset` is a club night with the venue in its name). The affinity filter on a
  personal board is the real filter; the town board is a listing of what is ticketed nearby and says which
  source it came from. If the town board proves noisy in P4, the fix is a *display* filter over published data
  or a category from a platform that has one — not a regex at ingest.
- **A cancellation is only detectable when a page says so.** Tickster renders `Aflyst` in its *listings*; the
  event page of a cancelled show carries no label and no purchase action. The adapter publishes the show as on
  sale and links out, rather than inferring a status from a missing button (a sold-out or not-yet-released show
  looks the same). Recorded here because it is a known wrongness, not an oversight.
- **Billetlugen is demoted from "Copenhagen's source" to "a listing that yields links".** Its event pages are
  403 to this client, exactly as Eventim's are, which is consistent: it is Eventim's Danish storefront (its
  assets are served from `DK-eventim` paths).

## 4. Alternatives rejected

- **Wait for Ticketmaster.** The account is in manual moderation, Denmark is not a Discovery market, and the
  ToU caps snapshot TTL at 24 h. A gate must not wait on a queue.
- **Scrape Copenhagen venues directly.** Measured earlier: 1 of 23 venue sites emits `Event` JSON-LD, 0 of 23
  publish ICS. That is a per-venue adapter for a fraction of one town.
- **A headless browser for Billetlugen's cards.** The listing hydrates dates client-side, so a browser would
  produce rows — and would also produce a 403 from the same edge as soon as it touched an event page. Building
  browser dependency into the miner for one source is a different architecture, not a smaller promise.
- **Invent a second source by counting a mirror or an aggregator of the same inventory.** Two rows from one
  feed is not corroboration; it inflates the duplicate rate in the flattering direction.

## 5. What would change this decision

- Billetto unblocking (a documented API or an explicit allowance) would restore a genuine second Copenhagen
  source and make the duplicate rate live again.
- A DICE or Eventim town inside Denmark or Sweden, which would create the first two-source town.
- The registry landing ticket-master-scale coverage for DK through a licensed agreement with a purge path.
