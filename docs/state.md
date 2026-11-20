# The board's state: two places, and nothing server-side

A board is a function of two things a person chose — a scope (a town, or everywhere) and a taste list — plus one
thing they did not: what was here last time. The choices go in the URL, because a board should be a link. The memory
stays in the browser, because it is about the visitor and not about the board.

## In the URL

```
https://lineup.messer.wtf/?town=stockholm-se&a=boiler-room,paul-van-dyk&n=paul-van-dyk:Paul%20van%20Dyk
https://lineup.messer.wtf/?town=anywhere&a=opeth&view=calendar
```

| Parameter | Meaning | Absent means |
|---|---|---|
| `town` | a town id from the manifest, or `anywhere` | the first published town |
| `a` | the taste list, as identity keys | no taste: the full board, no `yours` filter |
| `n` | `key:Name` for names a key cannot spell | every name is read back from its key |
| `view` | `calendar` | the list |
| `panel` | `taste` or `town` | no sheet |

`anywhere` is a **scope, not a town**: every published shard, deduped by row key, each row labelled with the town it
is actually in. It has no shard of its own, so it can only ever be asked for explicitly — by a link or a pick. Nothing
infers it: the first published town is the fallback.

Two rules the parameters follow:

- **A parameter is dropped when it matches the default.** `?view=list` is not written; clearing the taste list removes
  both `a` and `n`, so a board with no artists leaves no residue in the address bar.
- **A malformed link degrades, it does not crash.** A `?n=` pair whose escape is broken — the shape a chat app's URL
  truncation produces — is dropped, and the key in `a` still identifies the artist. Measured 2026-11-20: before the
  guard, `?n=viagra-boys:100%25` threw `URIError` at module scope and left the page blank.

## Which town opens

| Order | Source | When |
|---|---|---|
| 1 | `?town=` | the link names a town — a shared link always opens where it says |
| 2 | the visitor's own last pick | `localStorage` `lineup.town` |
| 3 | the first published town | a first visit |

Row 3 is the whole of it: there is **no edge hint and no IP guess** — `/where` was removed on 2026-11-20 because
nothing asked it, and a country is not a distance anyway. A first visit meets the gate, which asks for a town; the
first published town is only the fallback for a link with no `?town` and no stored pick.

## Anywhere, and distance

Rows print kilometres only when the board knows **both ends**: the venue's own position and where the reader is. A row
without one prints its venue — and, on `anywhere`, its town.

- **The near end is a device fix, never a guess.** It is memory-only (no prompt on load, absent without a secure
  context) and it is only ever produced by pressing `Use my location`.
- **A fix does not move a reader off `anywhere`.** The scope means everywhere, so a position only adds distances; the
  nearest-town promotion is skipped while the scope is `anywhere`, and the locate control says `Location on` rather than
  claiming a town was selected.
- **A distance is not published on `anywhere` unless both ends are known**: 355 of 718 Stockholm rows carry a venue
  position, 23 of 36 Copenhagen rows, and the rest print nothing.

## In the browser

| Key | What | Lifetime |
|---|---|---|
| `lineup.town` | the scope the reader picked (`stockholm-se`, `anywhere`) | until cleared |
| `lineup.taste` | `[{key,name}…]`, capped at 200 | until cleared |
| `lineup.taste.import` | `{k:"listenbrainz", u, keys}` — where a list came from | until the list is edited |
| `lineup.view` | `calendar` | until cleared |
| `lineup.onboarded` | the two-step gate has been seen | until cleared |
| `lineup.calendar` | the feed token the subscribe control last issued | until cleared |

`lineup.taste.import` is the provenance that lets a feed *follow* a handle, and it is dropped the moment the list no
longer equals what the import produced: one edit and the list is the reader's, not a provider's.

`lineup.calendar` is a bookmark of our own output, kept so the control can say **Update subscription** instead of
handing over a second calendar that duplicates the first.

## The calendar feed

`/calendar/<token>.ics` is base64url of `{v:1, t, f?, o?, a?, s?}`. Three properties matter and each was a defect
before it was a rule:

- **No window means the published window.** A token issued without `f`/`o` follows the manifest, so a subscription
  never ages out. Measured before the fix: a token whose window had passed answered **0 events** while the index held
  718 — and the client kept polling it for ever.
- **`t: "anywhere"`** reads every shard and writes each event's own town into `LOCATION`; one label for the whole feed
  would print `Debaser, Anywhere`.
- **`s: {k:"listenbrainz", u}`** is a live artist list, resolved at fetch time and cached six hours at the edge.
  `a` — the snapshot written at subscribe time — is the fallback, so a provider that is down, throttled or gone
  degrades to the list the subscriber saw. A source that resolves to nothing is an empty feed, never the whole town.

An empty feed is a valid calendar and a **standing watch**: the shows are live on every fetch, so a feed with zero
events today gains them the next time one of those artists announces a date. That is why the subscribe control is
offered at zero matches, and why its label counts what the feed holds rather than waiting for something to show.

## What this buys

- A personalised board is a **link**, not a session, so there is nothing to sign up for and nothing to delete.
- The server is never told any of it: no cookie, no session id, no taste row. The one exception is a feed's own token,
  which travels in the URL a calendar client already holds.
- `anywhere` is a second scope for the same price: no published "world" shard, no second data path — it is the artist
  route's union (read every shard, dedupe by `k`) applied to the board.
