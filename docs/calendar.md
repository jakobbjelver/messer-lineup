# The calendar feed

`/calendar/<token>.ics` is the board as a calendar a client can subscribe to. Everything else the Worker does is JSON;
this is the one route that renders.

## The token

base64url of a small JSON object:

```json
{ "v": 1, "t": "copenhagen-dk", "f": "2026-11-18", "o": "2027-03-18", "a": ["opeth"] }
```

| Key | Meaning |
|---|---|
| `v` | format version — a subscribed URL outlives a deploy, so a breaking change has to be visible |
| `t` | town id |
| `f` / `o` | the window, inclusive, `YYYY-MM-DD` |
| `a` | artist identity keys; absent means the whole town |

**It is not signed.** Plan §7.1 called for an HMAC, and that was written before the reason was clear: a signature proves
*we* issued the parameters, which matters only when the parameters grant something. These grant nothing — the feed is
the same public index, and anyone can already ask the board for any town, window or artist list. A signature would mean
holding a secret on the Worker for no gain, so the token is plain and the decoder is strict instead: a wrong version, an
unreadable day, a reversed window, an unknown field type or an over-long list is refused, never defaulted.

## The feed

| Decision | Why |
|---|---|
| times in **UTC** | the row carries the venue's offset, so converting once here means no `VTIMEZONE` to keep in step |
| `UID` = the row key | a subscriber re-fetches; a changing UID is a duplicate event. `artistKey\|venueKey\|date` is stable by construction |
| `DTEND` = start + **3 h** | the index knows when a show starts and nothing about when it ends. Three hours is a block a client draws, not a claim about the running time |
| `STATUS:CANCELLED` when `st=c` | the only status a calendar understands; `x`/`s` stay `CONFIRMED` because the show still happens |
| lines folded at 75 octets, text escaped | RFC 5545 §3.1 and §3.3.11. An unescaped comma in a venue truncates `LOCATION` |
| `Cache-Control: max-age=900` | short enough to follow a nightly publish, long enough that a mail client polling twice an hour does not reach origin |

A feed with no shows in its window is still a valid calendar — `BEGIN:VCALENDAR … END:VCALENDAR` with no `VEVENT` — because
a subscriber that receives a 404 starts showing an error instead of an empty month.

## Where the link comes from

The coverage panel builds it from what is on screen — town, window, the taste list — and offers it as a `webcal://`
link (which hands the URL to the OS calendar) with the plain `https://` address printed beside it for copying. Nothing
is stored server-side: the token *is* the state, which is why the same link works in any client on any device.

## Two runtimes, one shape

The feed is implemented twice on purpose — `src/calendar/` for the Worker and `internal/server/calendar.go` for the Go
dev server — and `scripts/contract.mjs` holds them together with a shape assertion (`kind: "ics"`): 200 +
`text/calendar`, balanced `VEVENT`s, `UID`/`DTSTAMP`/`DTSTART`/`DTEND`/`SUMMARY`/`LOCATION` in each, unique UIDs, CRLF,
no line over 75 octets. The *contents* differ by design, because each runtime reads its own index.

## Verified

- `make contract` — **10/10** agree, including the feed and an invalid token (both `400 invalid_calendar`).
- Go unit tests: the fixture's two Copenhagen shows render two events with UTC times and an escaped `LOCATION`; the
  artist filter keeps one; an empty window is a valid empty calendar; five kinds of garbage are refused; a 60-character
  rune-heavy summary folds without splitting a character and reassembles intact.
- An **independent parser** (`icalendar` 6.3.2, not our code) parsed the live feed: 37 events, unique UIDs, all UTC,
  every `DTEND` after its `DTSTART`, every event with `LOCATION` and `URL`, `X-WR-CALNAME: Messer Lineup — København`.
- In a browser: the panel's token decodes to the board on screen, and the feed it points at answers 200 `text/calendar`.

**Confirmed in a real client:** Jakob subscribed to the live feed in Apple Calendar and it imported without complaint
(2026-11-18). Google Calendar is still unverified — the structure is the same file a strict parser accepted, but "same
file" is not the same as "imported".
