# The job queue

A sweep fetches one detail page per candidate. A few of those fail for reasons that say nothing about the show — a 429,
a timeout, a gateway hiccup — and counting them as failures means the show is simply missing from a published window
until somebody happens to sweep that town again. The queue keeps those targets and retries them with backoff.

## What is queued

**Only transient failures.** The classification already existed in `internal/mine`; the queue reads it:

| Failure | Queued |
|---|---|
| `rate limited (429)`, `timeout`, network errors, `answered 5xx` | yes — a bad minute |
| `gone (404/410)` | no — the show was taken down |
| `blocked (403)`, `robots` | no — we are being refused |
| `no event block`, `no start date`, `no town`, `no venue` | no — the page is not (or is no longer) a show |

Retrying a fact spends the rate budget to learn it again.

## How it runs

```
sweep town T
  ├─ queued targets due for T are marked as retries
  │    ├─ not listed on the town page → prepended to this run's candidates
  │    └─ listed anyway             → counted as a retry, fetched once (never twice)
  ├─ a transient failure              → queued, first attempt 30 min later
  ├─ a retry that fails transiently   → attempt counted, backoff doubles
  └─ a retry that fails permanently   → dropped (it was a fact after all)
```

Backoff is 30 m, 1 h, 2 h, 4 h, then a 6 h cap. After **5** attempts the target is given up and a tombstone keeps it out
of the queue for **7 days** — without the tombstone the next sweep simply re-queues it, because the target is also an
ordinary candidate, and "five attempts" would mean five more every night.

A run reports what the queue did, and says nothing when it did nothing:

```
  copenhagen   tickster   52 candidates · 37 rows · 0 filtered · 1 failures · 29s · 1 retried, 0 queued, 1 given up
  queue                  1 retried · 0 queued for the next run · 1 given up
```

## Why the state file, not SQLite

Plan §1.2 specified SQLite, and `internal/state`'s own header reserved it for "when claims actually need transactions".
Nothing here needs a transaction: one process mines at a time, the queue holds a handful of entries, and a crash mid-run
costs at most a repeated fetch — which the backoff already tolerates. A database dependency would buy contention safety
nobody is contending for. The plan's *shape* is kept (claim by `not_before`, retry with backoff, never block the CLI) and
the storage is the honest one.

## Verified

- `internal/state`: dedupe by `source|kind|target`; due ordering and the `limit`; backoff 30 m → 1 h → 2 h → 4 h; give-up
  at five attempts; success removes; the queue survives a reopen.
- `internal/mine`: a 429 on run 1 queues one target and leaves the row out; the platform recovering means run 2 retries
  it and the row appears with the queue empty; a 403 is never queued; a target that keeps failing is given up exactly
  once and then stays out.
- **Against the real adapter**: a seeded queue with two due jobs (one 404, one real Tickster URL already in the index)
  ran 52 candidates → 37 rows, reported `2 retried · 1 given up`, left zero jobs, and did not double-count the show whose
  page the town listed anyway.
