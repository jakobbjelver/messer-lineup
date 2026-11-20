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
import { decodeEvent, decodeSpec, ANYWHERE } from "./calendar/spec";
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
// Registered before the 501 wildcard below: Hono matches in order, so a specific route declared after it is
// unreachable — the wildcard answers first and the search looks like a stub that was never built.
app.get("/api/v1/artists/search", async (c) => {
  const q = (c.req.query("q") ?? "").trim().replace(/\s+/g, " ").toLowerCase();
  if ([...q].length < 2) {
    return c.json({ artists: [] }, 200, { "cache-control": "public, max-age=21600" });
  }
  const cache = caches.default;
  // The key carries a version: a poisoned answer (an empty list from an upstream 503) cannot be purged from
  // `caches.default`, so a change to what may be cached has to move the key instead of waiting out the TTL.
  const key = new Request(`https://lineup.messer.wtf/api/v1/artists/search?v=2&q=${encodeURIComponent(q)}`);
  const hit = await cache.match(key);
  if (hit) {
    return new Response(hit.body, {
      headers: { "content-type": "application/json", "cache-control": "public, max-age=21600" },
    });
  }
  let body: { artists: ArtistHit[]; unavailable?: boolean };
  try {
    body = await musicBrainzSearch(q);
  } catch {
    body = { artists: [], unavailable: true };
  }
  const answer = new Response(JSON.stringify(body), {
    headers: {
      "content-type": "application/json",
      "cache-control": body.unavailable ? "no-store" : "public, max-age=21600",
    },
  });
  // An unavailable answer must not be cached: one 503 would otherwise be six hours of "no results" for every
  // visitor, served from the edge in milliseconds and indistinguishable from a search that found nothing.
  if (!body.unavailable) c.executionCtx.waitUntil(cache.put(key, answer.clone()));
  return answer;
});


/**
 * `/api/v1/artists/portraits?n=…` — a face for a search hit the published file has never heard of.
 *
 * The board's faces are published (`artists/portraits.json`) and cost nothing. A combobox row is a MusicBrainz hit
 * for an artist the index may have no rows for, which is why most of them wore initials. This asks Deezer — keyless,
 * one call, `picture_xl` — for at most eight names, and answers keyed by **the name the caller sent** so the two
 * runtimes never have to agree on the identity function to read one picture.
 *
 * The identity rule still applies: a returned artist whose folded name is not the name we asked about is refused,
 * because a namesake's face is worse than initials. A miss is cached for a day at the edge (the client keeps its own
 * for the session); a failure is neither cached nor claimed.
 */
const FACE_MAX = 8;
const FACE_GAP = 250;
const DEEZER_SEARCH = "https://api.deezer.com/search/artist?q=";

type Portrait = { u: string; c?: string };
type FaceBody = { portraits: Record<string, Portrait>; unavailable?: boolean };

/** A guard, not the published identity: NFKD, no marks, no quotes, `&` → and, credit cut, punctuation folded. */
function faceKey(name: string): string {
  let s = name.toLowerCase().normalize("NFKD").replace(/\p{M}/gu, "");
  s = s
    .replace(/ø/g, "o")
    .replace(/æ/g, "ae")
    .replace(/œ/g, "oe")
    .replace(/ß/g, "ss")
    .replace(/þ/g, "th")
    .replace(/ð/g, "d")
    .replace(/ł/g, "l")
    .replace(/đ/g, "d")
    .replace(/ı/g, "i")
    .replace(/&/g, " and ")
    .replace(/['’‘`´"“”„‚]/g, "");
  for (const marker of [" feat.", " ft.", " featuring "]) {
    const at = s.indexOf(marker);
    if (at >= 0) s = s.slice(0, at);
  }
  return s.replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
}

let dzChain: Promise<unknown> = Promise.resolve();
let dzLast = 0;

/** One Deezer search, serialised across requests so a burst of comboboxes cannot stack on the provider. */
async function deezerFace(name: string): Promise<{ portrait?: Portrait; failed?: boolean }> {
  const run = async () => {
    const wait = FACE_GAP - (Date.now() - dzLast);
    if (wait > 0) await new Promise((resolve) => setTimeout(resolve, wait));
    dzLast = Date.now();
    const res = await fetch(`${DEEZER_SEARCH}${encodeURIComponent(name)}&limit=1`, {
      headers: { "user-agent": "MesserLineup/0.2 (https://lineup.messer.wtf)", accept: "application/json" },
      signal: AbortSignal.timeout(8000),
    });
    if (res.status === 404) return {};
    if (!res.ok) return { failed: true };
    const body = (await res.json()) as {
      data?: { id?: number; name?: string; picture_xl?: string }[];
    };
    const artist = body.data?.[0];
    if (!artist?.picture_xl || !artist.name || !artist.id) return {};
    if (faceKey(artist.name) !== faceKey(name)) return {};
    return {
      portrait: { u: artist.picture_xl, c: `https://www.deezer.com/artist/${artist.id}` },
    };
  };
  const next = dzChain.then(run, run);
  dzChain = next.catch(() => undefined);
  return next as Promise<{ portrait?: Portrait; failed?: boolean }>;
}

async function deezerFaces(names: string[]): Promise<FaceBody> {
  const body: FaceBody = { portraits: {} };
  for (const name of names) {
    const answer = await deezerFace(name);
    if (answer.failed) body.unavailable = true;
    else if (answer.portrait) body.portraits[name] = answer.portrait;
  }
  return body;
}

app.get("/api/v1/artists/portraits", async (c) => {
  const seen = new Set<string>();
  const names: string[] = [];
  for (const raw of c.req.queries("n") ?? []) {
    const name = raw.trim().replace(/\s+/g, " ");
    const key = faceKey(name);
    if ([...name].length < 2 || [...name].length > 120 || !key || seen.has(key)) continue;
    seen.add(key);
    names.push(name);
    if (names.length === FACE_MAX) break;
  }
  if (names.length === 0) return c.json({ portraits: {} }, 200, { "cache-control": "public, max-age=86400" });

  const cache = caches.default;
  const query = names.map((name) => `n=${encodeURIComponent(name)}`).join("&");
  const key = new Request(`https://lineup.messer.wtf/api/v1/artists/portraits?v=1&${query}`);
  const hit = await cache.match(key);
  const headers = { "content-type": "application/json" };
  if (hit) return new Response(hit.body, { headers: { ...headers, "cache-control": "public, max-age=86400" } });

  const body = await deezerFaces(names);
  const answer = new Response(JSON.stringify(body), {
    headers: { ...headers, "cache-control": body.unavailable ? "no-store" : "public, max-age=86400" },
  });
  // Same rule as the search: a provider that blinked is not a fact about an artist, so it is not cached.
  if (!body.unavailable) c.executionCtx.waitUntil(cache.put(key, answer.clone()));
  return answer;
});

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
 * The token carries the whole board (town, taste) because a calendar client has no session to look up. Nothing in it
 * is secret: it names a slice of the same public index anyone can read, which is why it is not signed.
 *
 * Three rules, and each one was a defect before it was a rule:
 *
 *   - **No window means the published window.** A window frozen at subscribe time is a feed that dies — the client
 *     keeps polling and gets an empty calendar for ever. `f`/`o` are still honoured when a token carries them, so an
 *     older link keeps working.
 *   - **`anywhere` reads every shard** and labels each event with its own town. One label for the whole feed would
 *     print "Debaser, Anywhere", which is not a place.
 *   - **A live artist source is a convenience with a floor.** `s` is resolved here; `a`, the snapshot written at
 *     subscribe time, is what the feed falls back to when the provider is down, throttled or gone. A source that
 *     resolves to nothing is an empty feed, never the whole town.
 *
 * This is the only route that reads a shard itself, and it does so deliberately: a feed is rendered on demand by a
 * client we do not control, so there is no page to hand it a pre-rendered file. One manifest, one towns file and one
 * shard per town per request, all cacheable for fifteen minutes — a daily refresh across a handful of subscribers is
 * not a load.
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
      window: { from: string; to: string };
      towns: { town: string; shard: string }[];
    };

    const entries = spec.t === ANYWHERE ? manifest.towns : manifest.towns.filter((town) => town.town === spec.t);
    if (entries.length === 0) {
      return c.json(errorBody("unknown_town", `this index does not publish ${spec.t}`), 404);
    }

    // The window: the token's when it names one, the published one otherwise. This is what stops a subscription from
    // ageing out — the token no longer carries a deadline it cannot move.
    const from = spec.f ?? manifest.window.from.slice(0, 10);
    const to = spec.o ?? manifest.window.to.slice(0, 10);

    // The filter: an explicit list, or a live source that falls back to it. A source that resolves to nothing is an
    // empty feed, never the whole town — the subscriber asked for artists.
    let artists = spec.a && spec.a.length > 0 ? new Set(spec.a) : null;
    if (spec.s) {
      const live = await listenBrainzKeys(spec.s.u);
      artists = new Set(live ?? spec.a ?? []);
    }

    const labels = await townLabels(new URL("/towns.json", base));
    const shards = await Promise.all(entries.map((entry) => fetchShard(new URL(`/${entry.shard}`, base))));

    const rows: CalendarRow[] = [];
    for (let index = 0; index < entries.length; index += 1) {
      const entry = entries[index]!;
      const label = labels.get(entry.town) ?? entry.town;
      for (const row of shards[index]!) {
        const day = typeof row.d === "string" ? row.d.slice(0, 10) : "";
        if (!day || day < from || day > to) continue;
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
          // The event's own town, so an anywhere feed can say where each show is. A one-town feed writes the same
          // label as before, which is why its bytes do not change.
          town: label,
        });
      }
    }
    rows.sort((left, right) => (left.d === right.d ? (left.a === right.a ? (left.k < right.k ? -1 : 1) : left.a < right.a ? -1 : 1) : left.d < right.d ? -1 : 1));

    const anywhere = spec.t === ANYWHERE;
    const place = anywhere ? "Anywhere" : (labels.get(spec.t) ?? spec.t);
    const filtered = artists !== null;
    const count = artists?.size ?? 0;
    const word = count === 1 ? "artist" : "artists";
    const ics = buildIcs(rows, {
      townId: spec.t,
      townLabel: place,
      builtAt: manifest.built_at,
      origin: new URL(c.req.url).origin,
      calName: `Messer Lineup — ${place}`,
      calDesc: filtered
        ? `Shows for ${count} ${word}${anywhere ? ", anywhere" : ` in ${place}`}, from the Messer Lineup index.`
        : anywhere
          ? "Every show, in every published town, from the Messer Lineup index."
          : `Shows in ${place}, from the Messer Lineup index.`,
    });

    return c.body(ics, 200, {
      "content-type": "text/calendar; charset=utf-8",
      // A subscriber re-fetches on its own schedule; fifteen minutes is short enough to follow a nightly publish and
      // long enough that a mail client checking twice an hour does not reach origin.
      "cache-control": "public, max-age=900",
      "content-disposition": `inline; filename="lineup-${place.replace(/[^a-z0-9-]+/gi, "-").toLowerCase()}.ics"`,
    });
  } catch (err) {
    return c.json(errorBody("index_unavailable", err instanceof Error ? err.message : String(err)), 503);
  }
});

/**
 * A handle's top artists, for a feed whose token names a source.
 *
 * `null` means the provider did not answer — the caller must fall back to the token's snapshot. An empty array means
 * it answered with nothing (ListenBrainz replies `204` until an account's statistics have been computed), which is a
 * fact and not a failure. Six hours at the edge is four upstream calls a day for a subscriber that polls hourly.
 */
async function listenBrainzKeys(handle: string): Promise<string[] | null> {
  const user = handle.trim();
  if (!user) return null;
  const cache = caches.default;
  const key = new Request(`https://lineup.messer.wtf/api/v1/taste/listenbrainz?v=1&u=${encodeURIComponent(user)}`);
  const hit = await cache.match(key);
  if (hit) {
    const body = (await hit.json()) as { keys?: string[] };
    return Array.isArray(body.keys) ? body.keys : [];
  }
  try {
    const res = await fetch(
      `https://api.listenbrainz.org/1/stats/user/${encodeURIComponent(user)}/artists?range=year&count=200`,
      {
        headers: { "user-agent": "MesserLineup/0.2 (https://lineup.messer.wtf)", accept: "application/json" },
        signal: AbortSignal.timeout(8000),
      },
    );
    let keys: string[] | null = null;
    if (res.status === 204) {
      keys = [];
    } else if (res.ok) {
      const body = (await res.json()) as { payload?: { artists?: { artist_name?: string }[] } };
      const seen = new Set<string>();
      keys = [];
      for (const artist of body.payload?.artists ?? []) {
        const name = (artist.artist_name ?? "").trim();
        if (!name) continue;
        const identity = Key(name);
        if (!identity || seen.has(identity)) continue;
        seen.add(identity);
        keys.push(identity);
        if (keys.length === 200) break;
      }
    }
    if (keys === null) return null; // a moment, not a fact: nothing cached, the snapshot answers this request
    const answer = new Response(JSON.stringify({ keys }), {
      headers: { "content-type": "application/json", "cache-control": "public, max-age=21600" },
    });
    caches.default.put(key, answer.clone()).catch(() => undefined);
    return keys;
  } catch {
    return null;
  }
}

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

/**
 * Every published town's display name in one read.
 *
 * The shortest label wins, the same rule `internal/index/towns.go` applies: platforms name districts as often as
 * cities, and "København Ø" is a district. A feed reads this once and labels each event from it, because a feed that
 * spans towns cannot use one name for every row.
 */
async function townLabels(url: URL): Promise<Map<string, string>> {
  const labels = new Map<string, string>();
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(5000) });
    if (!res.ok) return labels;
    const body = (await res.json()) as { towns?: { id: string; name: string }[] };
    for (const town of body.towns ?? []) {
      if (!town.id || !town.name) continue;
      const current = labels.get(town.id);
      if (!current || town.name.length < current.length) labels.set(town.id, town.name);
    }
  } catch {
    /* a towns file that cannot be read leaves the ids: a feed titled `copenhagen-dk` is worse than one titled
       `København`, and better than a 503 for a cosmetic read. */
  }
  return labels;
}

/**
 * `/api/v1/artists/search?q=` — the artist picker's combobox, proxied.
 *
 * MusicBrainz sends `ACAO: *`, so the proxy is not about CORS: it is about the `503 busy` a burst earns
 * (measured), and about caching. One upstream call at a time, one per second at most, and six hours of cache per
 * normalised query — a combobox is exactly the client that would otherwise provoke the limiter.
 */
type ArtistHit = {
  name: string;
  mbid: string;
  type?: string;
  country?: string;
  disambiguation?: string;
  score?: number;
};

let mbChain: Promise<unknown> = Promise.resolve();
let mbLast = 0;

async function musicBrainzSearch(q: string): Promise<{ artists: ArtistHit[]; unavailable?: boolean }> {
  const run = async () => {
    const wait = 1100 - (Date.now() - mbLast);
    if (wait > 0) await new Promise((resolve) => setTimeout(resolve, wait));
    mbLast = Date.now();
    const res = await fetch(
      `https://musicbrainz.org/ws/2/artist?query=${encodeURIComponent(q)}&fmt=json&limit=6`,
      {
        headers: {
          // Their terms ask every client to identify itself; a browser UA cannot be set from a page, which is
          // another reason this call lives on the Worker.
          "user-agent": "MesserLineup/0.2 (https://lineup.messer.wtf)",
          accept: "application/json",
        },
        signal: AbortSignal.timeout(8000),
      },
    );
    if (!res.ok) return { artists: [], unavailable: true, hiccup: res.status >= 500 };
    const body = (await res.json()) as {
      artists?: { id?: string; name?: string; type?: string; country?: string; disambiguation?: string; score?: number }[];
    };
    const artists = (body.artists ?? [])
      .filter((a) => a.id && a.name && (a.score ?? 0) >= 60)
      .slice(0, 6)
      .map((a) => ({
        name: a.name!,
        mbid: a.id!,
        type: a.type?.toLowerCase(),
        country: a.country,
        disambiguation: a.disambiguation,
        score: a.score,
      }));
    return { artists };
  };
  // MusicBrainz throttles flashes of requests with a `503` + `retry-after: 1` under a **global** rate-limit zone
  // (measured 2026-11-20: five calls 1.2 s apart answered 200, 503, 503, 200, 503), so a 5xx is a hiccup and gets
  // two more asks; a transport failure is retried the same way, and a real outage still lands as `unavailable`.
  const attempt = async (): Promise<{ artists: ArtistHit[]; unavailable?: boolean; hiccup?: boolean }> => {
    try {
      return await run();
    } catch {
      return { artists: [], unavailable: true, hiccup: true };
    }
  };
  // Serialised across requests: the chain continues even after a failure, so one bad call cannot wedge it.
  const next = mbChain.then(attempt, attempt).then(async (first) => {
    let answer = first;
    for (let tries = 1; tries < 3 && answer.hiccup; tries++) answer = await attempt();
    return { artists: answer.artists, ...(answer.unavailable ? { unavailable: true } : {}) };
  });
  mbChain = next.catch(() => undefined);
  return next as Promise<{ artists: ArtistHit[]; unavailable?: boolean }>;
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
