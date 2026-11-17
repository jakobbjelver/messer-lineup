# Coverage — what each source actually measures

This file is the honesty record. It is written from a measurement, not from an intention: a source is
listed as reachable only if a request from the machine that will run the miner returned its content.
Regenerate it from `lineup mine --report` once the miner writes one (P0.13).

**Measured 2026-11-17 from the Mac mini, through `internal/fetch`** (full browser header profile, cookies
carried, `Referer` on click-through, robots honoured). Reproduce with `make probe`.

Byte sizes below are one sample; a dynamic page varies a little per request, which is why every
verdict is a status code and never a size.

## Sources

| Source | Market | Sitemap | Detail page | Verdict |
|---|---|---|---|---|
| Eventim (`www.eventim.de`, code `EVE`) | DE | **200** — index 5 100 B, 30 children; `events1…18`; shard 1 = 10 000 URLs, 188 071 B gzipped | **403** on 5 of 5 event pages (609–682 B, `Access Denied`, `errors.edgesuite.net`) | **Index only.** No date is reachable, so no row can be built. |
| AXS (`www.axs.com`) | UK, US, DE, FR, SE, AU, NZ, JP, SG | **200** — index 10 103 B, 81 children; `sitemap_event_uk` 11 784 043 B, 50 000 URLs | **403** on 3 of 3 event pages (≈107 KB bot wall) | **Index only.** |
| Billetlugen (`www.billetlugen.dk`) | DK | n/a (city pages, no sitemap) | **200**, 308 510 B of HTML | **Reachable.** |
| DICE (`dice.fm`) | UK, US, DE, … | per-city browse pages | **200**, 441 394 B, 3 `application/ld+json` blocks incl. `Event` | **Reachable**, structured data present. |
| Eventfrog (`www.eventfrog.de`) | DE, CH | city pages | **200**, 176 081 B, 71 `/event` links | **Reachable**, no JSON-LD — links must be parsed. |
| Billetto (`billetto.co.uk`) | UK | — | **202**, 2 040 B (challenge page) | Not reachable as a plain client. |
| Resident Advisor (`ra.co`) | UK | — | **403**, 768 B | Not reachable from here. |

## What this means for the global gate

G0-global asks for **Berlin ≥ 20, London ≥ 20, Austin ≥ 20** from keyless adapters, and named Eventim DE
for Berlin and AXS for London and Austin. Both of those serve their sitemaps and refuse their event
pages from this egress, so as measured the gate cannot be met with the candidates it names.

Two details worth keeping, because both have misled before:

- **`curl` and the Go client disagree on this host.** Eventim's home page answered 200 to the Go client
  and a TLS reset to `curl` within the same minute, with identical headers. TLS fingerprint, not
  reachability. Curl-only evidence is not evidence about the miner.
- **A cookie jar and a `Referer` did not change the outcome.** Eventim's event pages answered 403 with a
  session cookie from its own home page and `Sec-Fetch-Site: same-origin`. This is not a missing session;
  it is the edge refusing that page class to this client.

## Sources that are not a gate

Ticketmaster's Discovery key is optional and its account sits in manual moderation; nothing in this
product depends on it. It is listed here only so the absence is on the record rather than implied.
