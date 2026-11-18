# The board's state: two places, and nothing server-side

A board is a function of three things a person chose — a town, a taste list, a date window — plus one thing they
did not: what was here last time. The first three go in the URL, because a board should be a link. The fourth
stays in the browser, because it is about the visitor and not about the board.

## In the URL

```
https://lineup.messer.wtf/?town=stockholm-se&from=2026-12-01&to=2026-12-31&a=boiler-room,paul-van-dyk
```

| Parameter | Meaning | Absent means |
|---|---|---|
| `town` | the town id from the manifest | the first published town |
| `from` / `to` | a travel window, `YYYY-MM-DD`, clamped to what the index publishes | the miner's whole window |
| `a` | the taste list, as identity keys | no taste: the full board, no `yours` filter |

Everything else is derived. `reach=<km>` and `mute=<key>` are specified in the plan and deliberately absent until
there is something honest behind them: distance needs venue coordinates (Tickster publishes none) and mute needs
a per-source taste model (P5).

Two rules the parameters follow:

- **A parameter is dropped when it matches the default.** Selecting the whole season removes `from`/`to` rather
  than writing them, so a shared link is as short as the choice it represents.
- **A malformed window is ignored, not corrected.** `?from=2027-13-45` and `?from` after `to` both fall back to
  the published window; the board never invents a range.

## In the browser

| Key | What | Lifetime |
|---|---|---|
| `lineup.theme` | `dark` or `light`, applied before the first paint | until cleared |
| `lineup.seen` | `{seen: [k…], visited: YYYY-MM-DD}` — the rows that were on the board at the last visit | written on every load |

`lineup.seen` is what makes **"new since your last visit"** possible, and it is deliberately narrow:

- A row has **no "added" timestamp** — the nine-key contract has no field for one, and inventing one would make the
  board claim to know when the miner saw something it cannot know. So "new" means exactly *this show was not on the
  board when you last looked*, which is what a visitor means by it.
- **An empty memory marks nothing.** A first visit is not "everything is new" — the whole board is, and a page of
  accent bars teaches nothing.
- **A memory older than 30 days is discarded.** After a month the board is a different board, and every row would
  be marked, which is the same as marking none.
- At most 6000 keys are kept: enough for a heavy window, small enough to stay out of localStorage's way.

The marker itself is the 2px accent bar the design reserves for it (`.row .new`) — the same accent as the affinity
hairline, at the row's leading edge, and never a badge.

## What this buys

- A personalised board is a **link**, not a session, so there is nothing to sign up for and nothing to delete.
- A travel board is a link too: `?from=2026-12-01&to=2026-12-31` is exactly what someone planning a trip wants to
  send to a friend who is going with them.
- The server is never told any of it. There is no cookie, no session id, no `taste` row anywhere.

## Verified

Driven in a real browser against the live host:

```
copenhagen, full season            37 rows
copenhagen, 2026-11-18 → 12-31     24 rows · ?to=2026-12-31 (from dropped: it is the published start)
reload from that URL               24 rows — the link is the state
stockholm, December                698 → 345 rows · eyebrow "TRAVEL BOARD · 31 DAYS" · 01 dec … 31 dec
memory seeded with one row         36 of 37 marked new
memory cleared                     0 marked new
```
## Which town opens

The URL decides, and then it is a matter of who knows best:

| Order | Source | When |
|---|---|---|
| 1 | `?town=` | the link names a town — a shared link always opens where it says |
| 2 | the visitor's own last pick | stored in `localStorage` under `lineup.town.chosen` |
| 3 | `GET /where` — the edge's nearest town | a first visit, or after "Forget everything" |
| 4 | the first published town | no edge header, or a country the index does not cover |

**A pick outranks a guess on purpose.** Country is not distance: an address in Malmö is 28 km from Copenhagen and 500 km
from Stockholm, and `CF-IPCountry` calls both `SE`. The hint is only ever a guess for someone who has not said what they
want — once they have, the board remembers, and a wrong guess can never become sticky. Only an explicit pick is stored:
the hint is re-asked on every first visit, because a remembered guess would be indistinguishable from a remembered
choice.

### What the hint can and cannot know

`/where` takes each published town with the point the index knows for it — the mean of the venue positions its shows
publish, which Tickster states in the page's own map link, so the point is evidence rather than a gazetteer — and answers
with the nearest one, given the edge's `cf.latitude`/`cf.longitude`.

An address's position comes from its IP, and an IP is not an address. One home connection in Skåne geolocates to a point
11 km from Stockholm, which is a Stockholm answer for a visitor who is nowhere near it. That is not a bug to fix in the
board: it is the accuracy of the only location signal there is without asking permission. Which is precisely why the
memory outranks the hint, and why the hint is never stored.

The answer is `private` when it depended on the visitor's position — a shared cache must never hand one visitor's answer
to another — and `public` with `vary: CF-IPCountry` when it came from the country instead.

The URL is left clean when the board chose: `?town=` is written only when a link *named* a town that could not be
honoured, so a shared link never freezes one visitor's default into everyone else's address bar.

