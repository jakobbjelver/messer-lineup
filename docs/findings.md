# The findings ledger

The nightly is unattended. Every condition that matters is already detected somewhere — the yield guard, the publish
guard, the retry queue's give-up — but each of them only printed to a log at 04:10, and a log is not a place anyone
looks at 09:00.

A **finding** gives one of those conditions a name, the numbers it was decided on, and a lifecycle. The ledger lives at
`.lineup-state/findings.json`, beside the miner's state, and it is its own file: a corrupt ledger must not cost the rate
budget or the retry queue, and a run that touches no findings must not rewrite it.

## The rule: written where the condition is concluded

A finding is never produced by a second pass that re-derives the condition. Each writer is a place the run already knows:

| Kind | Written by | Evidence |
|---|---|---|
| `yield-collapse` | `lineup mine` — a source whose report carries a `Warning` (zero rows, a quarter of the town's own median, a walk that stopped early, a budget that ran out) | the warning text, candidates, rows, seconds |
| `queue-given-up` | `lineup mine` — a queued target that ran out of the five attempts | how many, and the candidates that run |
| `publish-refused` | `lineup publish` — the index did not verify, or the guards refused a collapsing index | the reason, the index directory, the problems |

A clean run writes nothing. A night that repeats a condition does not add a row: the ID is `kind|source|town`, so a
condition that lasts a week is one finding with a fresher timestamp and newer evidence.

## Closing is a person's act

A run that stops seeing the condition does **not** close it: "it stopped happening" is not "somebody dealt with it". A
closed finding that comes back re-opens, because the condition returned.

```
lineup findings list [--open] [--json]
lineup findings show <id> [--json]
lineup findings close <id> --note "why"
```

The ID is also the whole identity in `list`: `yield-collapse|tickster|stockholm` tells a reader what and where without a
second command.

## Where it shows up

- `lineup mine` prints `findings 4 raised · 3 open · .lineup-state/findings.json` after a run that raised or still holds
  anything, and nothing at all on a clean night.
- The nightly's last line carries the count: `… · published · 1 009 rows · 54 637 bytes · 2 152s · 0 findings · log …`.
- `last-run.json` carries `findings_open`, which is what `lineup-nightly-check` (a Hermes cron at 06:10) reads: a
  published night with findings is a good night for the board and a night that needs a look at the mine.

## Why not a field in `state.json`

The mining state is rewritten on every run and holds a rate budget, yield history and a retry queue. Findings are
event-shaped and read by a different consumer at a different time. Sharing a file would mean one corrupt ledger costs the
miner its memory, and every run rewrites a document it did not change.
