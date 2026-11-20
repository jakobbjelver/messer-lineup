#!/usr/bin/env node
/**
 * G1 — one contract, two runtimes.
 *
 * Sends the same requests to `lineup serve` (Go) and `wrangler dev` (the Worker) and diffs them. What is
 * compared depends on what the route is:
 *
 *   kind: "body"   the parsed JSON must be identical — for a deterministic route, which is every one that
 *                  takes its clock and its inputs as parameters.
 *   kind: "shape"  only the listed keys must exist on both — for a route whose body carries a clock or a live
 *                  read, where byte equality would assert on the moment of the call.
 *   kind: "ics"  both must answer 200 `text/calendar` with a structurally valid feed: balanced VEVENTs, a UID /
 *                DTSTART / DTEND / SUMMARY in each, unique UIDs, CRLF line endings, no line over 75 octets. The
 *                *contents* differ by design (each runtime reads its own index), the shape may not.
 *   kind: "document" both must answer 200 with an HTML document (a client route, not an asset and not a 404)
 *   kind: "error"  both must answer the same status with the same `error.code`. The message is free text and
 *                  names its runtime on purpose; the code is the contract.
 *
 * The API is the two artist routes and the calendar feed. Every other /api/v1 path answers the same 501 envelope on
 * both runtimes — an honest "not mine", and the only stub assertion this file needs.
 *
 * Usage: node scripts/contract.mjs [goBase] [workerBase]
 */

const GO = process.argv[2] ?? "http://127.0.0.1:8016";
const WORKER = process.argv[3] ?? "http://127.0.0.1:8788";

/** base64url of a board specification, built the way `web/src/lib/calendar.ts` builds it. */
const calendarToken = Buffer.from(
  JSON.stringify({ v: 1, t: "copenhagen-dk", f: "2026-11-01", o: "2027-01-31" }),
  "utf8",
).toString("base64").replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");

/**
 * A token with **no window**: the shape a subscription is issued in now. Absent `f`/`o` the feed follows the published
 * window, which is what stops a subscription from ageing out after its last day.
 */
const windowlessToken = Buffer.from(JSON.stringify({ v: 1, t: "copenhagen-dk" }), "utf8")
  .toString("base64")
  .replace(/\+/g, "-")
  .replace(/\//g, "_")
  .replace(/=+$/, "");

/** The reserved scope: every published town, each event labelled with its own. */
const anywhereToken = Buffer.from(JSON.stringify({ v: 1, t: "anywhere" }), "utf8")
  .toString("base64")
  .replace(/\+/g, "-")
  .replace(/\//g, "_")
  .replace(/=+$/, "");

/** Half a window, which is a link that lies about what it shows. */
const halfWindowToken = Buffer.from(JSON.stringify({ v: 1, t: "copenhagen-dk", f: "2026-11-01" }), "utf8")
  .toString("base64")
  .replace(/\+/g, "-")
  .replace(/\//g, "_")
  .replace(/=+$/, "");

const REQUESTS = [
  // Liveness: the clock is the whole point, so only its presence is compared.
  { method: "GET", path: "/healthz", compare: { kind: "shape", keys: ["ok", "as_of"] } },
  // Readiness reads a live index; the two runtimes answer from different sources (a local directory vs R2).
  { method: "GET", path: "/readyz", compare: { kind: "shape", keys: ["ok"] } },

  // A path this build does not serve: one honest 501 in the contract's envelope, not a 404. The API is the two
  // artist routes and the calendar feed; there is no account, no session and no taste provider beyond Last.fm.
  { method: "GET", path: "/api/v1/not-a-route", compare: { kind: "error", code: "not_implemented", status: 501 } },
  // The artist picker's search is real in both runtimes: one query that matches nothing, so the assertion is the
  // shape (an `artists` array) rather than an upstream result that could change between two calls.
  {
    method: "GET",
    path: "/api/v1/artists/search?q=zzzzqqq",
    compare: { kind: "shape", keys: ["artists"] },
  },
  // On-demand portraits: no names is an empty answer on both, and neither runtime may reach upstream for it.
  {
    method: "GET",
    path: "/api/v1/artists/portraits",
    compare: { kind: "body" },
  },
  // The taste import: one handle, one provider, one answer. Both runtimes ask Last.fm the same question, so the body
  // can be compared byte for byte — and a handle that does not exist must be the same 404 envelope on both. These two
  // are live reads with a key behind them: a machine without LASTFM_KEY reports them SKIPPED, never as agreement.
  { method: "GET", path: "/api/v1/taste/lastfm?user=messeratgalten", compare: { kind: "body" }, needsLastFM: true },
  {
    method: "GET",
    path: "/api/v1/taste/lastfm?user=zzzz-not-a-lastfm-handle",
    compare: { kind: "error", code: "unknown_user", status: 404 },
    needsLastFM: true,
  },
  // The one document route: both runtimes must answer an artist link with the SPA's document, not a 404. On the
  // Worker this goes script → ASSETS → `single-page-application`; on the Go server it is the history fallback.
  { method: "GET", path: "/a/four-tet", compare: { kind: "document" } },
  // The calendar feed is the only rendered route; its shape is the contract even when the rows behind it differ.
  { method: "GET", path: `/calendar/${calendarToken}.ics`, compare: { kind: "ics" } },
  // A subscription is issued without a window: both runtimes must follow the published one, and an anywhere feed must
  // label each event with its own town (`LOCATION:` is checked per event by the comparator).
  { method: "GET", path: `/calendar/${windowlessToken}.ics`, compare: { kind: "ics" } },
  { method: "GET", path: `/calendar/${anywhereToken}.ics`, compare: { kind: "ics" } },
  { method: "GET", path: "/calendar/not-a-token.ics", compare: { kind: "error", code: "invalid_calendar", status: 400 } },
  // Half a window is refused, not completed: that rule is the one a client-side encoder must not violate either.
  { method: "GET", path: `/calendar/${halfWindowToken}.ics`, compare: { kind: "error", code: "invalid_calendar", status: 400 } },
];

async function call(base, req) {
  const init = { method: req.method, headers: {} };
  if (req.body) {
    init.headers["content-type"] = "application/json";
    init.body = JSON.stringify(req.body);
  }
  try {
    const res = await fetch(new URL(req.path, base), init);
    const text = await res.text();
    let json = null;
    try {
      json = JSON.parse(text);
    } catch {
      /* a non-JSON body is compared as text */
    }
    return { status: res.status, json, text, type: res.headers.get("content-type") ?? "" };
  } catch (err) {
    return { status: 0, json: null, type: "", text: err instanceof Error ? err.message : String(err) };
  }
}

function pick(json, keys) {
  const out = {};
  if (!json || typeof json !== "object") return out;
  for (const key of keys) out[key] = key in json;
  return out;
}

/** Returns a list of differences; empty means the two runtimes agree. */
function diff(req, go, worker) {
  const problems = [];
  const { kind, contains } = req.compare;

  if (go.status !== worker.status) {
    problems.push(`status ${go.status} vs ${worker.status}`);
  }

  switch (kind) {
    case "ics": {
      for (const [label, res] of [
        ["go", go],
        ["worker", worker],
      ]) {
        const type = res.type ?? "";
        if (!type.includes("text/calendar")) problems.push(`${label}: content-type ${type || "(none)"}, want text/calendar`);
        const body = res.text ?? "";
        if (!body.endsWith("END:VCALENDAR\r\n")) problems.push(`${label}: feed does not end with END:VCALENDAR + CRLF`);
        if (!body.startsWith("BEGIN:VCALENDAR\r\n")) problems.push(`${label}: feed does not start with BEGIN:VCALENDAR`);
        // Every physical line obeys §3.1 — checked on the folded body, because folding is what the limit is about.
        for (const line of body.split("\r\n")) {
          if (new TextEncoder().encode(line).length > 75) problems.push(`${label}: a line exceeds 75 octets`);
        }
        // Everything below is read **unfolded**: a UID longer than 74 octets is folded, and comparing raw first lines
        // reports two different events as duplicates as soon as they share an artist and a venue.
        const unfold = body.replace(/\r\n /g, "");
        const events = unfold.split("BEGIN:VEVENT").length - 1;
        if (events !== unfold.split("END:VEVENT").length - 1) problems.push(`${label}: unbalanced VEVENTs (${events})`);
        const uids = new Set();
        for (const event of unfold.split("BEGIN:VEVENT").slice(1)) {
          const block = event.split("END:VEVENT")[0];
          for (const field of ["UID:", "DTSTAMP:", "DTSTART:", "DTEND:", "SUMMARY:", "LOCATION:"]) {
            if (!block.includes(field)) problems.push(`${label}: a VEVENT is missing ${field}`);
          }
          const uid = /UID:([^\r\n]+)/.exec(block)?.[1];
          if (uid) {
            if (uids.has(uid)) problems.push(`${label}: duplicate UID ${uid}`);
            uids.add(uid);
          }
        }
        if (uids.size !== events) problems.push(`${label}: ${events} events but ${uids.size} unique UIDs`);
        // A case may name lines the body must carry — the token's own event, spelled out, so a single-event route is
        // held to more than "some VEVENT parsed".
        for (const wanted of contains ?? []) {
          if (!unfold.includes(wanted)) problems.push(`${label}: the body is missing ${wanted}`);
        }
      }
      break;
    }
    case "document": {
      for (const [label, res] of [
        ["go", go],
        ["worker", worker],
      ]) {
        if (res.status !== 200) problems.push(`${label}: status ${res.status}, want 200`);
        if (!res.type.includes("text/html")) problems.push(`${label}: content-type ${res.type || "(none)"}, want text/html`);
        if (!/id="root"/.test(res.text)) problems.push(`${label}: the document has no #root`);
      }
      break;
    }
    case "shape": {
      const a = JSON.stringify(pick(go.json, req.compare.keys));
      const b = JSON.stringify(pick(worker.json, req.compare.keys));
      if (a !== b) problems.push(`shape ${a} vs ${b}`);
      break;
    }
    case "error": {
      const codeOf = (r) => r.json?.error?.code ?? null;
      if (codeOf(go) !== req.compare.code) problems.push(`go code ${codeOf(go)}`);
      if (codeOf(worker) !== req.compare.code) problems.push(`worker code ${codeOf(worker)}`);
      if (req.compare.status && go.status !== req.compare.status) {
        problems.push(`go status ${go.status} != ${req.compare.status}`);
      }
      break;
    }
    case "body": {
      if (JSON.stringify(go.json) !== JSON.stringify(worker.json)) problems.push("body differs");
      break;
    }
    default:
      problems.push(`unknown compare kind ${kind}`);
  }
  return problems;
}

const rows = [];
let failures = 0;
let skipped = 0;

const notConfigured = (res) => res.status === 503 && res.json?.error?.code === "not_configured";

for (const req of REQUESTS) {
  const [go, worker] = await Promise.all([call(GO, req), call(WORKER, req)]);

  if (go.status === 0 || worker.status === 0) {
    rows.push(`${req.method} ${req.path}  UNREACHABLE  go=${go.status} worker=${worker.status}`);
    failures += 1;
    continue;
  }

  // A route that needs a key neither runtime has is not a contract failure — but it is never counted as agreement
  // either: the line says SKIPPED, so a release check cannot mistake a keyless machine for a verified route.
  if (req.needsLastFM && notConfigured(go) && notConfigured(worker)) {
    skipped += 1;
    rows.push(`${req.method} ${req.path}  SKIPPED  LASTFM_KEY is not set on either runtime`);
    continue;
  }

  const problems = diff(req, go, worker);
  if (problems.length) {
    failures += 1;
    rows.push(`${req.method} ${req.path}  MISMATCH  ${problems.join("; ")}`);
    rows.push(`    go:     ${go.text.trim().slice(0, 150)}`);
    rows.push(`    worker: ${worker.text.trim().slice(0, 150)}`);
  } else {
    rows.push(`${req.method} ${req.path}  ok  ${go.status}`);
  }
}

console.log(rows.join("\n"));
console.log(
  `\nG1: ${REQUESTS.length - failures - skipped}/${REQUESTS.length - skipped} agree between ${GO} and ${WORKER}` +
    (skipped ? ` (${skipped} skipped: a key neither runtime has)` : ""),
);
process.exit(failures ? 1 : 0);
