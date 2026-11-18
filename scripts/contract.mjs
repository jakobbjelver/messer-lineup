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
 * Five of the six routes answer 501 until the taste providers land (P5). When one becomes real, flip its entry
 * to "body" — the assertion grows with the implementation, which is the point of a contract test.
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

const REQUESTS = [
  // Liveness: the clock is the whole point, so only its presence is compared.
  { method: "GET", path: "/healthz", compare: { kind: "shape", keys: ["ok", "as_of"] } },
  // Readiness reads a live index; the two runtimes answer from different sources (a local directory vs R2).
  { method: "GET", path: "/readyz", compare: { kind: "shape", keys: ["ok"] } },

  // The six (§5.6). All still stubs.
  {
    method: "GET",
    path: "/api/v1/taste/handle?p=lastfm&u=four-tet",
    compare: { kind: "error", code: "not_implemented", status: 501 },
  },
  { method: "GET", path: "/api/v1/auth/spotify/start", compare: { kind: "error", code: "not_implemented", status: 501 } },
  { method: "GET", path: "/api/v1/taste/me", compare: { kind: "error", code: "not_implemented", status: 501 } },
  {
    method: "POST",
    path: "/api/v1/taste/curate",
    body: { drop: ["four-tet|vega|2026-12-02"] },
    compare: { kind: "error", code: "not_implemented", status: 501 },
  },
  { method: "DELETE", path: "/api/v1/session", compare: { kind: "error", code: "not_implemented", status: 501 } },
  // The one document route: both runtimes must answer an artist link with the SPA's document, not a 404. On the
  // Worker this goes script → ASSETS → `single-page-application`; on the Go server it is the history fallback.
  { method: "GET", path: "/a/four-tet", compare: { kind: "document" } },
  // The calendar feed is the only rendered route; its shape is the contract even when the rows behind it differ.
  { method: "GET", path: `/calendar/${calendarToken}.ics`, compare: { kind: "ics" } },
  { method: "GET", path: "/calendar/not-a-token.ics", compare: { kind: "error", code: "invalid_calendar", status: 400 } },
  // Which town a visitor lands on. Both runtimes answer from the same header, so the shape (and the fallback) agree.
  {
    method: "GET",
    path: "/where?t=copenhagen-dk@55.6761,12.5683&t=stockholm-se@59.3293,18.0686",
    compare: { kind: "shape", keys: ["town", "basis", "km"] },
  },
  { method: "GET", path: "/where", compare: { kind: "error", code: "bad_request", status: 400 } },
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
  const { kind } = req.compare;

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
        const events = body.split("BEGIN:VEVENT").length - 1;
        if (events !== body.split("END:VEVENT").length - 1) problems.push(`${label}: unbalanced VEVENTs (${events})`);
        const uids = new Set();
        for (const event of body.split("BEGIN:VEVENT").slice(1)) {
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
        for (const line of body.split("\r\n")) {
          if (new TextEncoder().encode(line).length > 75) problems.push(`${label}: a line exceeds 75 octets`);
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

for (const req of REQUESTS) {
  const [go, worker] = await Promise.all([call(GO, req), call(WORKER, req)]);

  if (go.status === 0 || worker.status === 0) {
    rows.push(`${req.method} ${req.path}  UNREACHABLE  go=${go.status} worker=${worker.status}`);
    failures += 1;
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
console.log(`\nG1: ${REQUESTS.length - failures}/${REQUESTS.length} agree between ${GO} and ${WORKER}`);
process.exit(failures ? 1 : 0);
