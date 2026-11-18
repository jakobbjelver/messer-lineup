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

TOWNS="${LINEUP_TOWNS:-copenhagen,stockholm}"
STATE="${LINEUP_STATE:-.lineup-state}"
INDEX="${LINEUP_INDEX:-.lineup-index}"
LEDGER="${LINEUP_LEDGER:-$HOME/Artifacts/messer-lineup/ledger.md}"
LOG_DIR="${LINEUP_LOG_DIR:-$HOME/Artifacts/messer-lineup/logs}"
ENV_FILE="${LINEUP_ENV_FILE:-$HOME/.config/messer-lineup/env}"

stamp() { date "+%Y-%m-%d %H:%M"; }
day() { date "+%Y-%m-%d"; }

mkdir -p "$LOG_DIR" "$(dirname "$LEDGER")"
LOG="$LOG_DIR/nightly-$(day).log"
# Everything goes to the log; exactly one line goes to the caller (a cron job delivers that verbatim).
exec 3>&1 1>>"$LOG" 2>&1

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
if ! ./bin/lineup mine --source tickster --town "$TOWNS" --index "$INDEX" --state "$STATE/state.json" --report "$REPORT"; then
  echo "nightly: mine failed; nothing was published"
  printf '| %s | %s | mine failed | — | — |\n' "$(stamp)" "$TOWNS" >>"$LEDGER"
  printf 'lineup nightly %s · mine failed · log %s\n' "$(stamp)" "$LOG" >&3
  exit 1
fi
TOOK=$(( $(date +%s) - START ))

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

printf 'lineup nightly %s · %s · %s rows · %s bytes · %ss · log %s\n' \
  "$(stamp)" "$RESULT" "$ROWS" "$BYTES" "$TOOK" "$LOG" >&3
