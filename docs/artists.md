# Artist names

A ticketing platform publishes a **title**, not a performer. Tickster has no `performer` field and Nortic's `name` is
the poster headline, so the artist column carries two kinds of string:

- a name — `Opeth`, `Lisa Nilsson`, `Bo Kaspers Orkester`
- a title with a name inside it — `Amanda Ginsburg - Det är jul nu igen`, `FÄLTET kl 19.00`,
  `FUNERAL | THE CRYPT`, `När det lider mot jul - Lisa Nilsson kl 16.00`

A title is not a bad row — the gig is real and the ticket link works — but its key matches no listener's list and
resolves to no genre, so it has to be cleaned **before** the merge and the publish.

## The rule

`internal/resolve` decides one question per string: which fragment, if any, is the artist. It is deliberately
conservative, because a wrong name is worse than an untidy one.

1. **The whole string first.** If the string as published is a real artist (`Kaizers Orchestra - Live` can be a band's
   name), keep it and split nothing.
2. **Otherwise the fragments.** Split on ` - `, ` – `, ` — `, `|`, `: `, ` med ` and ` with `. Accept the result only
   when **exactly one** fragment is a real artist. Two real fragments is a co-bill — choosing one would drop the other
   from the board.
3. **Exact name only.** The matcher asks Deezer's keyless artist search and accepts a result only when the folded name
   is identical. Deezer answers a query with its best guesses; `Aural Onsdag` returned **`Aura Onda`**, which is a
   different artist.
4. **A failure is not a decision.** A transport error or a 5xx is counted and retried on a later run; it is never
   stored as "no such artist".
5. **A negative expires; a positive does not.** An undecided name is asked again after 30 days (a band can register a
   profile later); a resolved name is final until an operator corrects it.

Then the pass runs **before `match.Merge`**, so the cleaned name is the key that dedupes and that the index publishes.

## What it measured — 2026-11-22

The first full sweep with the resolver (four towns, both sources, `--resolve-limit 0`):

| | before | after |
|---|---:|---:|
| published rows | 1 017 | **1 009** |
| artist keys | 869 | 844 |
| distinct names | 891 | 866 |
| names with a title shape | 237 | **122** |
| rows with a title shape | 268 | **143** |
| duplicate rate | 3.5 % | **4.1 %** (43 merged, +7 from cleaner keys) |

235 names were asked, 113 resolved, 122 stayed undecided, and **140 rows were rewritten**. The board lost 8 rows
because seven extra duplicate spellings folded into one show — `Amanda Ginsburg - Det är jul nu igen` and
`Amanda Ginsburg` at the same room and day are one gig.

The remaining 143 rows are mostly co-bills (`FUNERAL | THE CRYPT`), which the rule refuses by design, and titles whose
only artist is not on Deezer.

## The nine operator corrections

Reviewing the 113 decisions by eye found **nine** where the title, not the act, had been chosen: `Celtic Christmas`
(act: West of Eden), `Queen of the Night` (a Whitney Houston show), `Burt Bacharach` (a tribute concert),
`The Greatest Gift` (a Christmas concert), `Babylon Berlin` (a 1930s party), `Bamse` (a children's character show) and
`Resurrection` (a label night). They are marked undecided in `.lineup-state/artists.json` with
`why: "operator: the title is not the act"`, so they are not re-asked and the next sweep publishes their raw titles.

The class is **a title that is also somebody's artist name** — and the ` med `/` with ` candidates close the clearest
half of it: `Celtic Christmas - En julkonsert med West of Eden` now offers both `Celtic Christmas` and `West of Eden`,
both resolve, and "exactly one" refuses the row. A decision count is therefore reported with its audit: roughly 8 % of
the first batch needed an operator.

## The hit rate is not re-measured here

The two lists behind the earlier `0/42` and `0/17` figures were never written down, so they cannot be re-run — record
the exact names beside the number next time. Cleaning a key cannot create a date: the lists were classic rock and
Americana with no Swedish room dates in the window, and the resolver changes keys, not calendars.

## Operations

```sh
lineup mine --resolve-limit 0          # resolve every new name this run
lineup mine --no-resolve               # measure the sources alone
lineup mine --resolve-limit 120        # the nightly's budget (default)
```

Decisions live in `.lineup-state/artists.json` (`--artist-state`), beside the miner's state. To audit:

```sh
python3 - <<'PY'
import json
state = json.load(open('.lineup-state/artists.json'))['map']
for key, decision in sorted(state.items()):
    if decision.get('to'):
        print(f"{decision['to']:<40} ← {key}")
PY
```

To refuse a decision, set `to` to `""` and `why` to `operator: <the reason>`. The row keeps its published string and
the name is not asked again for 30 days.
