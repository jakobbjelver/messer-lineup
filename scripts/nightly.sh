#!/usr/bin/env bash
#
# The nightly run: rebuild the miner, sweep the towns, publish what verifies, write one ledger line.
#
# Why a script and not a Worker or a GitHub Action: the miner is a host process (the design's "a private miner
# publishes the index; Cloudflare only serves it"), and GitHub's ToS rules out scheduled scraping in Actions.
#
# It is deliberately boring and it fails loudly:
#   - `make build` first, because a stale binary is the failure this project has already paid for twice;
#   - the sweep writes to .lineup-index and only `lineup publish` puts anything public;
#   - the publish guards (plan §5.8) refuse a collapsing index, so a broken parser means a quiet night, not a
#     quiet board;
#   - one ledger line per run, whether it published or refused.
#
# Credentials live in ~/.config/messer-lineup/env (0600), never in the repo.

set -euo pipefail

cd "$(dirname "$0")/.."

TOWNS="${LINEUP_TOWNS:-copenhagen,stockholm,gothenburg,malmo}"
STATE="${LINEUP_STATE:-.lineup-state}"
INDEX="${LINEUP_INDEX:-.lineup-index}"
LEDGER="${LINEUP_LEDGER:-$HOME/Artifacts/messer-lineup/ledger.md}"
LOG_DIR="${LINEUP_LOG_DIR:-$HOME/Artifacts/messer-lineup/logs}"
ENV_FILE="${LINEUP_ENV_FILE:-$HOME/.config/messer-lineup/env}"
COVERAGE="docs/coverage.md"
# The one machine-readable thing a watcher reads. A log is for a human reading history; this is for asking
# "did last night work?" without parsing prose.
STATUS_FILE="$HOME/Artifacts/messer-lineup/last-run.json"

stamp() { date "+%Y-%m-%d %H:%M"; }
day() { date "+%Y-%m-%d"; }

# write_status <state> <detail> [rows] — the machine-readable answer to "did last night work?".
#
# It is written atomically (temp + rename) because a watcher may read it at any moment, and a half-written JSON file
# reads as a broken run. The `running` line is written before the sweep starts: a night that takes 40 minutes must be
# distinguishable from a night that never happened, and a check that fires mid-run must say "still running" rather than
# report the previous night as tonight's success.
write_status() {
  local state="$1" detail="$2" rows="${3:-0}"
  python3 - "$STATUS_FILE" "$state" "$detail" "$rows" "$TOWNS" "$LOG" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" <<'PY'
import json, os, sys
path, state, detail, rows, towns, log, at = sys.argv[1:8]
tmp = path + ".tmp"
with open(tmp, "w") as handle:
    json.dump(
        {"state": state, "detail": detail, "rows": int(rows or 0), "towns": towns, "log": log, "at": at},
        handle,
    )
    handle.flush()
    os.fsync(handle.fileno())
os.replace(tmp, path)
PY
}

mkdir -p "$LOG_DIR" "$(dirname "$LEDGER")"
LOG="$LOG_DIR/nightly-$(day).log"
# Everything goes to the log; exactly one line goes to the caller (a cron job delivers that verbatim).
exec 3>&1 1>>"$LOG" 2>&1

write_status "running" "the sweep is running" 0
echo "=== nightly $(stamp) · towns $TOWNS"

if [ -f "$ENV_FILE" ]; then
  # shellcheck disable=SC1090
  set -a; . "$ENV_FILE"; set +a
else
  echo "nightly: no $ENV_FILE — publishing will be refused (the guards need credentials to read the bucket)"
fi

make --silent build

REPORT="$INDEX/sweep-$(day).json"
START=$(date +%s)
# `--coverage` is what keeps the public honesty record equal to the run that published: the block is generated, so
# the only way it can drift is if the producer is not asked for it (it was hand-refreshed on 2026-11-20 for exactly
# that reason).
if ! ./bin/lineup mine --source tickster,nortic --town "$TOWNS" --index "$INDEX" --state "$STATE/state.json" \
    --report "$REPORT" --coverage "$COVERAGE"; then
  echo "nightly: mine failed; nothing was published"
  printf '| %s | %s | mine failed | — | — |\n' "$(stamp)" "$TOWNS" >>"$LEDGER"
  write_status "mine-failed" "the sweep failed; nothing was published"
  printf 'lineup nightly %s · mine failed · log %s\n' "$(stamp)" "$LOG" >&3
  exit 1
fi
TOOK=$(( $(date +%s) - START ))

# One file, one commit. A generated block that is never committed is a record that drifts, and a nightly that
# leaves the worktree dirty breaks the next pull — so the run that measured it commits it, and nothing else.
if ! git diff --quiet -- "$COVERAGE"; then
  git add "$COVERAGE" &&
    git -c user.name="Messer nightly" -c user.email="nightly@messer.wtf" \
      commit -q -m "docs: the coverage record follows the nightly ($(day))" &&
    echo "nightly: committed $COVERAGE"
fi

# Portraits are their own artifact and their own budget: a slow MusicBrainz lookup must never hold the rows back,
# and a failure here is not a reason to skip the publish — initials are a supported state. The cap keeps a nightly
# run bounded; the state file makes progress compound across nights.
if ! ./bin/lineup portraits --index "$INDEX" --state "$STATE/portraits.json" --limit 80; then
  echo "nightly: portraits failed; publishing without a portrait refresh"
fi

# Publish only what verifies and only what the guards allow. A refusal is a result, not an incident: the published
# index stays, which is the whole point of content-addressed shards.
if ./bin/lineup publish; then
  RESULT="published"
else
  RESULT="refused (the published index is unchanged)"
fi

read -r TOWN_COUNT ROWS BYTES SHA <<<"$(python3 - "$INDEX/manifest.json" "$INDEX" <<'PY'
import json, os, sys
manifest_path, index_dir = sys.argv[1], sys.argv[2]
m = json.load(open(manifest_path))
print(
    len(m["towns"]),
    sum(t["count"] for t in m["towns"]),
    sum(os.path.getsize(os.path.join(index_dir, t["shard"])) for t in m["towns"]),
    (m["towns"][0]["sha256"][:7] if m["towns"] else "-"),
)
PY
)"

if [ ! -f "$LEDGER" ]; then
  printf '# Messer Lineup — publish ledger\n\n| when | towns | what | time | result |\n|---|---|---|---|---|\n' >"$LEDGER"
fi
printf '| %s | %s | %s towns · %s rows · %s bytes · %s | %ss | %s |\n' \
  "$(stamp)" "$TOWNS" "$TOWN_COUNT" "$ROWS" "$BYTES" "$SHA" "$TOOK" "$RESULT" >>"$LEDGER"

write_status "$([ "$RESULT" = "published" ] && echo published || echo refused)" \
  "$RESULT · $TOWN_COUNT towns, $ROWS rows, ${BYTES} bytes, ${TOOK}s" "$ROWS"

printf 'lineup nightly %s · %s · %s rows · %s bytes · %ss · log %s\n' \
  "$(stamp)" "$RESULT" "$ROWS" "$BYTES" "$TOOK" "$LOG" >&3
