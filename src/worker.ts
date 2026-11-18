/**
 * The Messer Lineup Worker.
 *
 * Assets are served first by the platform — a page view never invokes this script, which is what keeps it
 * free and unmetered. The script therefore only answers what cannot be a file: `/healthz`, `/readyz`, the
 * five `/api/v1` routes that need an account, and `/calendar/<spec>.ics`.
 *
 * The index is *not* proxied. It lives in the R2 bucket behind `lineup-data.messer.wtf` and the board fetches
 * it directly, so a shard never travels through a Worker and never costs CPU.
 *
 * The routes that need accounts answer 501 in the contract's error envelope until the taste providers land
 * (P5). That is deliberate: the envelope and the status are the contract `scripts/contract.mjs` diffs against
 * `lineup serve`, so a client written today will not need changing when the route becomes real.
 */

import { Hono } from "hono";
import { buildEvent, buildIcs, type CalendarRow } from "./calendar/ics";
import { decodeEvent, decodeSpec } from "./calendar/spec";
// The identity key is the client's own implementation, imported rather than re-implemented: a third copy of a
// normalisation rule is a third place for it to drift, and `make keycheck` already proves this one against
// `internal/model/testdata/keys.json`.
import { Key } from "../web/src/lib/key";

export interface Env {
  /** `https://lineup-data.messer.wtf` — where the built index is published. Read only by /readyz. */
  DATA_BASE: string;
  /**
   * The static assets, bound so the one client route can be resolved without moving page views onto the script.
   * `/a/<artist key>` has no file behind it; the asset layer's `single-page-application` rule answers it with
   * index.html once the script asks it to.
   */
  ASSETS: Fetcher;
}

const app = new Hono<{ Bindings: Env }>();

/** The only error shape this Worker produces: `{"error":{"code","message"}}`. */
function errorBody(code: string, message: string) {
  return { error: { code, message } };
}

app.get("/healthz", (c) =>
  // Liveness only: the process answers. Whether the index is usable is /readyz's question.
  c.json({ ok: true, as_of: new Date().toISOString() }),
);

app.get("/readyz", async (c) => {
  // The edge cannot run the Go verifier, so readiness is the one thing it can honestly check: the manifest
  // the board starts from is reachable. A bucket that is down and a bucket that is empty both fail here,
  // which is the right answer for a load balancer and the wrong one for a human — hence the `detail`.
  const base = c.env.DATA_BASE;
  if (!base) {
    return c.json({ ...errorBody("not_configured", "DATA_BASE is not set on this Worker"), ok: false }, 503);
  }
  try {
    const res = await fetch(new URL("/manifest.json", base), {
      method: "GET",
      headers: { "cache-control": "no-cache" },
      signal: AbortSignal.timeout(5000),
    });
    if (!res.ok) {
      return c.json({ ok: false, detail: `manifest ${res.status}` }, 503);
    }
    const manifest = (await res.json()) as { format?: string; towns?: unknown[] };
    return c.json({
      ok: true,
      format: manifest.format ?? "unknown",
      towns: Array.isArray(manifest.towns) ? manifest.towns.length : 0,
    });
  } catch (err) {
    return c.json({ ok: false, detail: err instanceof Error ? err.message : String(err) }, 503);
  }
});

/**
 * `/where?towns=a-dk,b-se` — which town a visitor should land on.
 *
 * The board is static, so it cannot know a country; the edge can, for free. The client sends the published town ids
 * and gets back the one whose country suffix matches `CF-IPCountry` (every id is `<slug>-<cc>` by construction), or the
 * first one when nothing matches. No index read, no state, and the answer is cached per country for a day — so this
 * costs one Worker invocation per country per day, not per visitor.
 */
/**
 * `/where?t=<id>@<lat>,<lon>&t=<id>@<lat>,<lon>` — which town a visitor should land on.
 *
 * The board is static, so it cannot know where a request came from; the edge can. Each entry is a published town and,
 * when the index knows it, that town's own point (the mean of the venue positions its shows publish). With a point for
 * the visitor and a point for each town, the answer is the nearest one — because a country is not a distance: an address
 * in Malmö is 28 km from Copenhagen and 500 km from Stockholm, and `CF-IPCountry` calls both `SE`.
 *
 * Without coordinates (no geo header, or a town whose venues never stated a position) the country suffix decides, then
 * the first published town. And because a nearest answer is about one visitor, it must never be cached for another:
 * `public` only when the answer did not depend on where the request came from.
 */
app.get("/where", (c) => {
  const entries = (c.req.queries("t") ?? [])
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0)
    .slice(0, 50)
    .map(parseTownEntry)
    .filter((entry): entry is TownEntry => entry !== null);
  if (entries.length === 0) {
    return c.json(errorBody("bad_request", "t is required: the published towns, as id@lat,lon"), 400);
  }

  const cf = c.req.raw.cf as { country?: string; latitude?: string; longitude?: string } | undefined;
  const country = (cf?.country ?? c.req.header("cf-ipcountry") ?? "").toLowerCase();
  const here = point(Number(cf?.latitude), Number(cf?.longitude));

  let chosen: TownEntry = entries[0]!;
  let basis = "fallback";
  if (here) {
    let best = Number.POSITIVE_INFINITY;
    for (const entry of entries) {
      if (!entry.at) continue;
      const distance = haversine(here, entry.at);
      if (distance < best) {
        best = distance;
        chosen = entry;
        basis = "distance";
      }
    }
  }
  if (basis === "fallback" && country) {
    const match = entries.find((entry) => entry.id.endsWith(`-${country}`));
    if (match) {
      chosen = match;
      basis = "country";
    }
  }

  const headers: Record<string, string> =
    basis === "distance"
      ? { "cache-control": "private, max-age=86400" }
      : { "cache-control": "public, max-age=86400", vary: "CF-IPCountry" };
  return c.json(
    {
      town: chosen.id,
      country: country || null,
      basis,
      km: basis === "distance" ? Math.round(haversine(here!, chosen.at!) / 100) / 10 : null,
    },
    200,
    headers,
  );
});

interface TownEntry {
  id: string;
  at: { lat: number; lon: number } | null;
}

function parseTownEntry(raw: string): TownEntry | null {
  const [id, coords] = raw.split("@");
  if (!id || id.length > 64) return null;
  return { id, at: coords ? point(Number(coords.split(",")[0]), Number(coords.split(",")[1])) : null };
}

function point(lat: number, lon: number): { lat: number; lon: number } | null {
  if (!Number.isFinite(lat) || !Number.isFinite(lon)) return null;
  if (lat < -90 || lat > 90 || lon < -180 || lon > 180) return null;
  if (lat === 0 && lon === 0) return null; // "unknown", not the Gulf of Guinea
  return { lat, lon };
}

/** Great-circle distance in metres. */
function haversine(a: { lat: number; lon: number }, b: { lat: number; lon: number }): number {
  const R = 6_371_000;
  const rad = Math.PI / 180;
  const dLat = (b.lat - a.lat) * rad;
  const dLon = (b.lon - a.lon) * rad;
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a.lat * rad) * Math.cos(b.lat * rad) * Math.sin(dLon / 2) ** 2;
  return 2 * R * Math.asin(Math.sqrt(h));
}

// The five JSON routes from plan §5.6, plus the calendar feed. Until P5 they answer honestly rather than
// pretending to be a 404: the board can tell "not mine" from "not yet".
app.all("/api/v1/*", (c) =>
  c.json(
    errorBody(
      "not_implemented",
      `${new URL(c.req.url).pathname} arrives with the taste providers (P5); this Worker serves the index only`,
    ),
    501,
  ),
);

/**
 * `/calendar/<token>.ics` — the board as a subscribable feed.
 *
 * The token carries the whole board (town, window, artist keys) because a calendar client has no session to look up.
 * Nothing in it is secret: it names a slice of the same public index anyone can read, which is why it is not signed.
 *
 * This is the only route that reads a shard itself, and it does so deliberately: a feed is rendered on demand by a
 * client we do not control, so there is no page to hand it a pre-rendered file. One manifest, one towns file and one
 * shard per request, all cached for fifteen minutes — a daily refresh across a handful of subscribers is not a load.
 */
/**
 * `/event/<token>.ics` — one show, as a calendar file.
 *
 * The token carries the event itself, so this route reads nothing: no manifest, no shard, no town lookup. It is the same
 * `VEVENT` the feed writes for that row, with the same `UID`, so a show taken from a row and the same show arriving
 * through a subscription are one event to the client rather than two.
 */
app.get("/event/:file", (c) => {
  const file = c.req.param("file");
  if (!file.endsWith(".ics")) {
    return c.json(errorBody("not_found", "a single event is /event/<token>.ics"), 404);
  }
  const spec = decodeEvent(file.slice(0, -4));
  if (!spec) {
    return c.json(errorBody("invalid_event", "that link is not a show"), 400);
  }
  const ics = buildEvent(
    { k: spec.k, a: spec.a, v: spec.v2, d: spec.d, u: spec.u, st: "o" },
    { townId: "", townLabel: spec.n, builtAt: new Date().toISOString(), origin: new URL(c.req.url).origin },
  );
  return c.body(ics, 200, {
    "content-type": "text/calendar; charset=utf-8",
    // The event is in the URL, so the answer can never change for that URL.
    "cache-control": "public, max-age=3600",
    "content-disposition": `attachment; filename="${file.replace(/\.ics$/, "")}.ics"`,
  });
});

app.get("/calendar/:file", async (c) => {
  const file = c.req.param("file");
  if (!file.endsWith(".ics")) {
    return c.json(errorBody("not_found", "a calendar feed is /calendar/<token>.ics"), 404);
  }
  const spec = decodeSpec(file.slice(0, -4));
  if (!spec) {
    return c.json(errorBody("invalid_calendar", "that link is not a board specification"), 400);
  }
  const base = c.env.DATA_BASE;
  if (!base) {
    return c.json(errorBody("not_configured", "DATA_BASE is not set on this Worker"), 503);
  }

  try {
    const manifestRes = await fetch(new URL("/manifest.json", base), {
      headers: { "cache-control": "no-cache" },
      signal: AbortSignal.timeout(5000),
    });
    if (!manifestRes.ok) {
      return c.json(errorBody("index_unavailable", `manifest ${manifestRes.status}`), 503);
    }
    const manifest = (await manifestRes.json()) as {
      built_at: string;
      towns: { town: string; shard: string }[];
    };
    const entry = manifest.towns.find((town) => town.town === spec.t);
    if (!entry) {
      return c.json(errorBody("unknown_town", `this index does not publish ${spec.t}`), 404);
    }

    const [shardRows, townName] = await Promise.all([
      fetchShard(new URL(`/${entry.shard}`, base)),
      townLabel(new URL("/towns.json", base), spec.t),
    ]);

    const artists = spec.a ? new Set(spec.a) : null;
    const rows: CalendarRow[] = [];
    for (const row of shardRows) {
      const day = typeof row.d === "string" ? row.d.slice(0, 10) : "";
      if (!day || day < spec.f || day > spec.o) continue;
      if (typeof row.a !== "string") continue;
      if (artists && !artists.has(Key(row.a))) continue;
      // The nine-key contract, checked rather than asserted: a row missing any of these is not an event.
      if (
        typeof row.k !== "string" ||
        typeof row.v !== "string" ||
        typeof row.d !== "string" ||
        typeof row.u !== "string"
      ) {
        continue;
      }
      rows.push({
        k: row.k,
        a: row.a,
        v: row.v,
        d: row.d,
        u: row.u,
        st: typeof row.st === "string" ? row.st : "o",
      });
    }

    const ics = buildIcs(rows, {
      townId: spec.t,
      townLabel: townName,
      builtAt: manifest.built_at,
      origin: new URL(c.req.url).origin,
    });

    return c.body(ics, 200, {
      "content-type": "text/calendar; charset=utf-8",
      // A subscriber re-fetches on its own schedule; fifteen minutes is short enough to follow a nightly publish and
      // long enough that a mail client checking twice an hour does not reach origin.
      "cache-control": "public, max-age=900",
      "content-disposition": `inline; filename="lineup-${spec.t}.ics"`,
    });
  } catch (err) {
    return c.json(errorBody("index_unavailable", err instanceof Error ? err.message : String(err)), 503);
  }
});

/** A shard is stored gzipped; a runtime that already decompressed it hands back JSONL, and both are read. */
async function fetchShard(url: URL): Promise<Record<string, unknown>[]> {
  const res = await fetch(url, { signal: AbortSignal.timeout(10000) });
  if (!res.ok) throw new Error(`shard ${res.status}`);
  const buffer = await res.arrayBuffer();
  const bytes = new Uint8Array(buffer);
  let text: string;
  if (bytes.length > 2 && bytes[0] === 0x1f && bytes[1] === 0x8b) {
    text = await new Response(
      new Blob([buffer]).stream().pipeThrough(new DecompressionStream("gzip")),
    ).text();
  } else {
    text = new TextDecoder().decode(bytes);
  }
  const rows: Record<string, unknown>[] = [];
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    try {
      rows.push(JSON.parse(trimmed) as Record<string, unknown>);
    } catch {
      /* a malformed line is skipped, exactly as the board skips it */
    }
  }
  return rows;
}

/** The town's display name, falling back to the id: a feed titled `copenhagen-dk` is worse than one titled `København`. */
async function townLabel(url: URL, id: string): Promise<string> {
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(5000) });
    if (!res.ok) return id;
    const body = (await res.json()) as { towns?: { id: string; name: string }[] };
    const labels = (body.towns ?? []).filter((town) => town.id === id).map((town) => town.name);
    // The shortest label wins, the same rule `internal/index/towns.go` applies: platforms name districts as often as
    // cities, and "København Ø" is a district.
    return labels.sort((a, b) => a.length - b.length)[0] ?? id;
  } catch {
    return id;
  }
}

app.notFound((c) => {
  // Assets are served before the script, so reaching this means the path is neither a file nor an API route. Two
  // different things can be true: a document route the SPA renders (`/a/<key>`), or a path that does not exist.
  // Hand the request back to the asset layer and let its `single-page-application` rule decide — it answers with
  // index.html for a client route and with the 404 page for a path that is genuinely nothing.
  const method = c.req.method;
  if (method === "GET" || method === "HEAD") {
    return c.env.ASSETS.fetch(c.req.raw);
  }
  return c.json(errorBody("not_found", "no such route"), 404);
});

app.onError((err, c) => {
  console.error("worker error", err instanceof Error ? err.stack : err);
  return c.json(errorBody("internal", "something broke"), 500);
});

export default app;
