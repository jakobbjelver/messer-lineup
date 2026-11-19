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
| `reach` | a distance in km from the visitor, one of `5`, `15`, `50` | anywhere in the town |

Everything else is derived. `mute=<key>` is specified in the plan and deliberately absent until there is something
honest behind it: a per-source taste model is P5. `reach` used to be in that sentence for the same reason, and is not
any more — see below.

Two rules the parameters follow:

- **A parameter is dropped when it matches the default.** Selecting the whole season removes `from`/`to` rather
  than writing them, and "anywhere" removes `reach`, so a shared link is as short as the choice it represents.
- **A malformed window is ignored, not corrected.** `?from=2027-13-45` and `?from` after `to` both fall back to the
  published window; the board never invents a range. A `reach` that is not one of the three stops is ignored the same
  way — the stops are the whole vocabulary, so a link cannot carry a distance the panel cannot display.

## Distance, and the one thing it needs

Rows print kilometres only when the board knows **both ends**: the venue's own position, and where the reader is.

- **The far end is the row's, and it is optional.** A venue position is published by some sources and not others
  (measured 2026-11-19: 23 of 37 Copenhagen rows, 355 of 693 Stockholm). A row without one prints its venue and
  nothing after it — the same rule the Go side already follows when it marshals `g`: *a row without coordinates
  prints no distance instead of a wrong one.*
- **The near end is a device fix, never a guess.** Browser GPS is the only signal that knows where a person is, so it
  is the only origin a distance is measured from. The town's own point (the mean of its venues) chooses a *town*; it
  never prints kilometres, because "2.4 km from the middle of Copenhagen" is not a sentence anyone asked for.
- **The fix is never stored.** It lives in the tab's memory and dies with it: nothing writes it to `localStorage`, the
  URL or a request. There is no prompt on load — the palette's `Use my location` row is the only trigger, and it is
  absent entirely in a non-secure context, where the browser cannot answer.
- **A refusal is a state.** `denied` is reported in the panel in one sentence, nothing retries on its own, and the
  town list is still the answer.

`reach=` narrows the board to what can be proven inside the distance. That has one consequence the board must say out
loud: a row with no position cannot be judged, so it leaves the view and is **counted** rather than kept as if it had
passed — the coverage panel prints "14 more have no venue position, so no distance can be measured for them", and the
line above the board counts what the reach left ("23 shows · 1 source · within 5 km").

A link can carry a reach to a device that has not granted a fix. The board then shows the whole town and says so in
the line — `<n> km reach needs your location` — rather than printing a count that looks filtered and is not.

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

**A guess wears `approx.`** — the small chip beside the town name, present exactly while rows 3 and 4 of that table
answered. A fix from `Use my location` clears it, and the page then opens on the nearest published town computed from
the fix itself; a link that named a town keeps it, because the visitor asked for that town and that outranks where they
are standing.

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

