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

## Why the provider rows are absent, not disabled

`Add taste` ranks a music account first, and that is still the right ranking: it is the only route that stays
current as taste moves. But every provider route needs a server to hold a token and a client secret, and that is
P5. A row that cannot do its job is a lie about what tapping it will do, so the panel shows what works and says
plainly that accounts arrive with the backends:

> A music account and a Last.fm handle arrive with the taste backends — they need a server to hold the token, and
> neither exists yet. Pasting is the faster route anyway.

The rows slot in above the paste row when they land; the panel does not change shape.

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
- **No server round-trip.** There is no session, no taste row and no `/taste/curate`: the list lives in the URL
  and in localStorage, and the only server route a taste import uses is `GET /api/v1/taste/lastfm`.
- **No "new since last visit"** — the marker was retired with the 2026-11-20 rethink; nothing stores a visit.
