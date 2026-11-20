# Taste: how the board becomes yours, and why nothing is stored

The board is complete without any of this. Taste **sharpens** it; it never gates it. That promise is the reason
the first taste input to ship is the one that needs no account at all.

## What exists today

**Type or paste artists.** The list is parsed in the browser (`web/src/lib/taste.ts`), reduced to identity keys
by the same function the miner uses (`web/src/lib/key.ts`, checked against `internal/model/testdata/keys.json`
by `make keycheck`), and matched against the shard the board already has. A row whose artist is in the set is
marked (`data-mine`, the affinity hairline) and the `yours` filter appears.

Nothing is sent anywhere. The list lives in the URL:

```
https://lineup.messer.wtf/?town=copenhagen-dk&a=boiler-room,paul-van-dyk
```

which makes a personalised board a link you can send, and makes "forget my taste" a matter of clearing the
parameter. No cookie, no session, no server-side list.

## The two imports, and why there are no more

`Import from` offers **ListenBrainz** and **Last.fm** — one handle field and one button each:

- **ListenBrainz** is keyless and answers the tab directly (`stats/user/<handle>/artists`, asked for the board's
  200).
- **Last.fm** goes through our own origin (`GET /api/v1/taste/lastfm?user=`), because `user.getTopArtists` needs
  the app's API key and a key in a browser is a key in every visitor's tab. The route answers names for the last
  twelve months — the same window ListenBrainz is asked for — deduped by identity key, held six hours at the edge,
  and it tells a handle Last.fm does not know (`404 unknown_user`) apart from one where the provider blinked
  (`503`, never cached). Last.fm reports the first in the **body** on a 404, which is why the route reads the body
  before it believes the status.

A music account (Spotify, YouTube, Apple) needs OAuth, a session and a server-side token — the shape ADR-004
decided against: Spotify's development mode is five allowlisted users with a Premium owner at any scale, and a
control most visitors cannot connect is a control that cannot do its job. The Spotify **export file** is the
exception that needs no account at all, and it is the next taste step.

## The identity contract is shared, not duplicated

`key` decides whether "Björk" in a pasted list is the "Björk" on the board. Two implementations exist — Go for
the merge, TypeScript for the client — and `internal/model/testdata/keys.json` is canonical for both:

```
make keycheck   # keycheck: 30/30 vectors agree with internal/model/key.go
```

The vectors are never edited to make an implementation pass. That is the same rule the merge follows, and it is
why a name that normalises to nothing (`¥$`) still has a stable identity instead of an empty key that would join
every unknown artist to every other one.

## What is deliberately not here

- **No curation weighting for pasted lists.** A provider list arrives ordered by listening weight; a pasted one
  has no weight to invent, so every chip counts the same and the chips with a show on the current board are
  marked instead (`{n} on this board`). Inventing weights would make the affinity hairline a decoration.
- **No server round-trip.** There is no session and no taste row: the list lives in the URL and in localStorage,
  and the only request an import makes carries the handle and nothing else (`GET /api/v1/taste/lastfm`). Nothing
  server-side remembers who asked.
- **No "new since last visit"** — the marker was retired with the 2026-11-20 rethink; nothing stores a visit.
