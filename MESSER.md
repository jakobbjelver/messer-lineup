# MESSER.md — the board's adoption record

**Surface:** `lineup.messer.wtf` — the board (`/`), an artist's dates (`/a/<key>`), the first-run gate.
**Canon:** messer-ui 2.5.0 · brand `lineup` (`MicrophoneStage`, acid lime `oklch(.52 .17 128)` / `oklch(.84 .19 128)`).
**Port:** 1 (canon atoms, patched locally where §4 records it).

## What this surface is

One screen: the gigs near you by the artists you care about. The board is a day-grouped list; the same rows
are a month calendar when the reader asks for it; two questions (artists, town) are asked once and are
settings afterwards.

## Accent budget (the one rule the whole design is built on)

Exactly one hue, 128, and it appears as **light, not paint**. At rest at most four things carry it:

1. the town sign (the page's `h1`), whose glow exists only in the dark theme — on paper the accent is ink,
   because a light at L .84 on a paper background is 1.35:1 and reads as a stain;
2. the primary button of the current step, exactly one per view;
3. the location control while no fix exists (a granted fix stops the glow: a light that outlives its action
   lies about the next tap);
4. the avatar ring on a row by one of the reader's artists, and only while the board shows everyone.

Everything else is neutral: hover is one surface step, focus is 2 px `--ring`, selected is an accent tint plus
a 2 px rule. Status text (`cancelled`, `sold out`) is `--foreground`, not a hue — the board has no status
colours at all, which is why `--success`/`--warning`/`--destructive` are absent from its tokens.

Measured on the live page (2026-11-20): one chromatic hue family (HSL 83–88, i.e. oklch 128) across every
computed `color`/`background`/`border`/`outline`/`box-shadow`; **one** element with an accent shadow on the
board (the location control), three during the first-run gate.

## Deviations from canon, each with its reason

| § | Deviation | Why |
|---|---|---|
| §16 | No sheet/card: the rows sit on the page. | The board is a document, not a panel. One hairline does the structure a frosted card was doing, and the card's shadow rule stopped being the page's largest visual element. |
| §11 | Rows are 68 px, not a 28/32/36 control. | A row is a link with two lines of text and a 40 px face; the density rule is for controls. |
| §12 | Copy is one sentence per state, and the error names no architecture. | "Nothing is broken — the next sweep replaces the shard" was a note to us, not to a reader. |
| §15 | No tooltips; the artist credit is a line of text on the artist page. | A credit is not a hint. |
| §9 | Phosphor, 16 px, `weight="regular"`; `fill` is never used. | Canon rule 9; the previous hand-rolled 87-line icon set is gone. |
| §17 | Glow is a four-part shadow on **coloured light only**, never on static type. | The sign's bloom is a duplicate at 40 % under the glyphs, not a `text-shadow`. |

## Portraits

`artists/portraits.json` (`{ artistKey: { u, c } }`) is published beside the shards, immutable. Sources, best
coverage first: **Deezer's public artist search** (`picture_xl`, one call, no key) → MusicBrainz `image`
relations → the artist's Wikipedia lead image. MusicBrainz hosts no images of its own; its relations are links
to Commons and most artists have none, which is why it cannot be the primary. The UI treats a portrait as an
enhancement: an avatar is initials first, the face replaces it when it loads, and a build with no portraits is
a supported state. An identity mismatch is never allowed to become a portrait — a bill (`A + B + C`) keeps its
initials, and a Wikipedia page whose own description does not name music is refused. Takedown is
`.lineup-state/blocklist.json` (`{"keys": […]}`): the pipeline skips those keys and the next publish removes
them.

Credit: the artist page links the source under the name; the footer carries
"Portraits · Wikimedia Commons" only when the published file has entries.

## What the board does not do

No season window, no travel dates, no reach, no "forget everything", no account, no OAuth, no calendar
subscription in the UI (the `.ics` endpoints remain as an API). The first-run gate is escapable: back or
Escape commits a guest profile rather than trapping a shared link.
