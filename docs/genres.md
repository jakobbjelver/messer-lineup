# Genres

Sixteen words, mapped from the tags listeners already put on an artist, so a board can be a sound as well as a list of
names.

## The vocabulary

`rock · metal · punk · pop · electronic · hip-hop · folk · jazz · soul · blues · country · classical · reggae · world ·
experimental · soundtrack`

The list lives in `internal/genres` (Go) and `src/calendar/genres.ts` (both JS runtimes); the ids are the wire form and
the labels are the display. A picker caps at **six** — six words is a taste, sixteen is a dump.

## The mapping

Last.fm's `artist.getTopTags` is the source, and it is not a taxonomy: tags are whatever listeners typed. Three rules:

- **A stoplist first.** `swedish`, `svenskt`, `seen live`, `favorites`, `female vocalists`, `eurovision`, decades and
  the other non-genres are dropped before mapping.
- **An exact map, then a substring map, in a fixed order.** `pop` cannot become `rock` through `progressive`; `folk
  rock` is folk; `synth pop` is pop. Metal is checked first, because `metal` alone must never fall through to rock.
- **The first three surviving tags decide, in the provider's weight order.** Opeth's tags (progressive metal → metal,
  progressive death metal → metal, … progressive rock → rock) are `[metal, rock]`; Robyn's are `[pop, electronic]`;
  Thåström's (swedish, singer-songwriter, rock, punk) are `[folk, rock, punk]`. Those three cases are tests.

A tag outside the vocabulary maps to nothing and is **counted**, and the count is in the report. The list grows from
evidence — `docs/genres.md` is not a place to keep a fourth opinion.

## The artifact

`lineup genres` reads every published shard, asks Last.fm about the artists it has never seen (bounded by `--limit`,
1 req/s), and writes `artists/genres-<sha>.json` through `internal/artifact`, which names it in the manifest's `files`
map:

```json
{
  "format": "lineup-genres/1",
  "built_at": "2026-11-22T12:00:00Z",
  "artist": {"opeth": ["metal", "rock"]},
  "genres": {"metal": ["opeth", "katatonia"]}
}
```

Both directions, because the board asks *what genre is this artist* per row and the feed asks *which artists are metal*
once. The raw tags are cached in `.lineup-state/genres.json` with the time they were fetched: a positive never expires,
a negative is re-asked after 30 days. The artifact is content-addressed, so a new night is a new name and a returning
visitor is never served last week's map.

## On the board and in the feed

- **The board** is a union: a row is "yours" when its artist is in the list **or** its artist plays a chosen genre.
  `?g=metal,punk` is the link, `lineup.genres` is the memory, and a board with no artists but two genres is a board.
- **The feed's token** carries `g` and `v:2` (a `v:1` token can never carry it). The server's artist set is
  `a ∪ ⋃ genres[g]`; an unknown genre contributes nothing, so a genre token that matches nothing is an **empty
  calendar, never the town**. The description names the genres (`Shows in Metal, Punk in Stockholm, …`).
- A source (`s`) still overrides `a`; the genres are added to whatever it resolved to. A provider that is down falls
  back to the snapshot, exactly as before.

## Coverage, measured 2026-11-22

The first full run asked Last.fm about all **844** published artists:

| | |
|---|---:|
| artists published | 844 |
| the provider knows (has tags) | 278 |
| **mapped to a genre** | **264 — 95 % of the artists the provider knows** |
| of all published artist strings | 31 % |

The gap is the artist field, not the mapping: **566 artist strings (67 %) have no Last.fm page at all**, because they are
headlines rather than acts — `A Zero8 Christmas`, `Abonnemang Stockholm — våren 2027`, `A Tribute To Lemmy Kilmister`.
`docs/artists.md` measures the same thing from the other side. The mapping maps what the provider knows; no vocabulary
can map a name that is not a name.

The spread is plausible: rock 134, pop 81, electronic 68, folk 59, jazz 35, punk 34, metal 31, soul 24, hip-hop 23,
blues 18, experimental 15, country 10, world 7, reggae 6, classical 4, soundtrack 2. A metal-only board is **38 rows**
(Stockholm 18, Gothenburg 11, Malmö 6, Copenhagen 3) and the feed returns exactly those 18 for Stockholm.

**The caveat is the name.** A genre is a name match, and Last.fm has namesakes: *Boiler Room* is a club night here and a
band there, *Across the River* is a Springsteen tribute and a stoner band. The feed's `g` is therefore as good as the
artist identity, not better — which is why the row still carries its own name and link, and why the next lever is artist
identity, not a longer synonym list.

A build with no artifact at all is supported: the board falls back to artists alone and a genre feed is empty rather
than wrong.
