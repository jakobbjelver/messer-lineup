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
import { buildIcs, type CalendarRow } from "./calendar/ics";
import { decodeSpec, ANYWHERE } from "./calendar/spec";
import { genreLabel } from "./calendar/genres";
// The identity key is the client's own implementation, imported rather than re-implemented: a third copy of a
// normalisation rule is a third place for it to drift, and `make keycheck` already proves this one against
// `internal/model/testdata/keys.json`.
import { Key } from "../web/src/lib/key";

export interface Env {
  /** `https://lineup-data.messer.wtf` — where the built index is published. Read only by /readyz. */
  DATA_BASE: string;
  /**
   * The Last.fm API key behind the taste import. Set with `wrangler secret put LASTFM_KEY`; it never reaches the
   * tab, because the route that uses it is also the route that caches what the provider said.
   */
  LASTFM_KEY?: string;
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

// Every route that exists has a consumer: the two JSON routes the board calls, the feed a calendar client
// subscribes to, and the 501 wildcard below for anything else (an honest "not mine", not a 404 lie).
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

/**
 * `/api/v1/taste/lastfm?user=` — one handle's top artists, for the picker's import control.
 *
 * Last.fm is where most listeners already are, and `user.getTopArtists` is a public read whose API key is the
 * app's identity. The key therefore lives here and never reaches the tab. The body is **names**, the currency the
 * import already speaks, so the identity function does not need a second implementation at the edge.
 *
 * Last.fm reports failure in the **body** with HTTP 200 (`{"error":6,"message":"User not found"}`), so a client that
 * trusts the status calls a missing handle a success. Three outcomes, three statuses: names (200, six hours),
 * no such user (404, remembered for a day), provider blinked (503, `no-store`, never remembered).
 */
const LASTFM_API = "https://ws.audioscrobbler.com/2.0/";
const LASTFM_GAP = 200;
const LASTFM_MAX = 200;
const LASTFM_PERIOD = "12month";

type LastFMTop = { names: string[]; answer: "ok" | "unknown" | "failed" };

let lfmChain: Promise<unknown> = Promise.resolve();
let lfmLast = 0;

/** One call, serialised across requests: Last.fm's own limit is per key, and a picker is a burst by nature. */
async function lastfmTopArtists(key: string, user: string): Promise<LastFMTop> {
  const run = async (): Promise<LastFMTop> => {
    const wait = LASTFM_GAP - (Date.now() - lfmLast);
    if (wait > 0) await new Promise((resolve) => setTimeout(resolve, wait));
    lfmLast = Date.now();
    const url = new URL(LASTFM_API);
    url.searchParams.set("method", "user.getTopArtists");
    url.searchParams.set("user", user);
    url.searchParams.set("api_key", key);
    url.searchParams.set("format", "json");
    url.searchParams.set("limit", String(LASTFM_MAX));
    url.searchParams.set("period", LASTFM_PERIOD);
    const res = await fetch(url, {
      headers: { "user-agent": "MesserLineup/0.2 (https://lineup.messer.wtf)", accept: "application/json" },
      signal: AbortSignal.timeout(8000),
    });
    // **Last.fm's error envelope rides on a non-2xx status**: a missing user is `404` with
    // `{"error":6,"message":"User not found"}` (measured). So the body decides, and the status only speaks when the
    // body carries no error code — otherwise a named answer is reported as "did not answer".
    if (res.status >= 500) return { names: [], answer: "failed" };
    const body = (await res.json().catch(() => null)) as {
      error?: number;
      topartists?: { artist?: { name?: string }[] };
    } | null;
    if (!body) return { names: [], answer: "failed" };
    if (body.error) return { names: [], answer: body.error === 6 ? "unknown" : "failed" };
    if (res.status !== 200) return { names: [], answer: "failed" };
    const seen = new Set<string>();
    const names: string[] = [];
    for (const artist of body.topartists?.artist ?? []) {
      const name = (artist.name ?? "").trim();
      if (!name) continue;
      const identity = Key(name);
      if (!identity || seen.has(identity)) continue;
      seen.add(identity);
      names.push(name);
      if (names.length === LASTFM_MAX) break;
    }
    return { names, answer: "ok" };
  };
  const next = lfmChain.then(run, run);
  lfmChain = next.catch(() => undefined);
  return next;
}

app.get("/api/v1/taste/lastfm", async (c) => {
  const key = c.env.LASTFM_KEY;
  if (!key) {
    // A build fact, not an outage: name the setting instead of calling the provider with no identity.
    return c.json(errorBody("not_configured", "LASTFM_KEY is not set on this Worker"), 503, { "cache-control": "no-store" });
  }
  const user = (c.req.query("user") ?? "").trim();
  if (!user || [...user].length > 64) {
    return c.json(errorBody("invalid_user", "user is required, and names a Last.fm handle"), 400);
  }

  const cache = caches.default;
  const cacheKey = new Request(`https://lineup.messer.wtf/api/v1/taste/lastfm?v=1&u=${encodeURIComponent(user)}`);
  const hit = await cache.match(cacheKey);
  const headers = { "content-type": "application/json" };
  if (hit) return new Response(hit.body, { headers: { ...headers, "cache-control": "public, max-age=21600" } });

  const answer = await lastfmTopArtists(key, user);
  if (answer.answer === "unknown") {
    return c.json(errorBody("unknown_user", "Last.fm has no user by that name"), 404);
  }
  if (answer.answer === "failed") {
    // Nothing cached and nothing claimed: the next try asks again. Six hours of "no artists" for one 503 is the
    // exact bug the search route already paid for.
    return c.json(errorBody("provider_unavailable", "Last.fm did not answer; try again"), 503, { "cache-control": "no-store" });
  }
  const res = new Response(JSON.stringify({ artists: answer.names }), {
    headers: { ...headers, "cache-control": "public, max-age=21600" },
  });
  c.executionCtx.waitUntil(cache.put(cacheKey, res.clone()));
  return res;
});

app.all("/api/v1/*", (c) =>
  c.json(
    errorBody(
      "not_implemented",
      `${new URL(c.req.url).pathname} is not a route this build serves; the API is the artist routes, the taste import and the calendar feed`,
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
      files?: Record<string, string>;
    };

    const entries = spec.t === ANYWHERE ? manifest.towns : manifest.towns.filter((town) => town.town === spec.t);
    if (entries.length === 0) {
      return c.json(errorBody("unknown_town", `this index does not publish ${spec.t}`), 404);
    }

    // The window: the token's when it names one, the published one otherwise. This is what stops a subscription from
    // ageing out — the token no longer carries a deadline it cannot move.
    const from = spec.f ?? manifest.window.from.slice(0, 10);
    const to = spec.o ?? manifest.window.to.slice(0, 10);

    // The filter: an explicit list, a live source that falls back to it, and the genres unioned in. A source that
    // resolves to nothing is an empty feed, never the whole town — the subscriber asked for artists.
    const genreIDs = spec.g ?? [];
    let artists: Set<string> | null = null;
    if (spec.s) {
      const live = await listenBrainzKeys(spec.s.u);
      artists = new Set(live ?? spec.a ?? []);
    } else if (spec.a && spec.a.length > 0) {
      artists = new Set(spec.a);
    }
    if (genreIDs.length > 0) {
      // A genre-only token is a board. The artifact is named by the manifest; a build that has not published one yet
      // contributes nothing, which makes the feed empty rather than the whole town.
      const name = manifest.files?.genres;
      artists ??= new Set();
      if (name) {
        const fromGenres = await genreKeys(new URL(`/${name}`, base));
        if (fromGenres === null) {
          return c.json(errorBody("index_unavailable", "genres"), 503);
        }
        for (const id of genreIDs) {
          for (const key of fromGenres[id] ?? []) artists.add(key);
        }
      }
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
      reminder: spec.r === 1,
      calName: `Messer Lineup — ${place}`,
      calDesc:
        genreIDs.length > 0
          ? `Shows in ${genreIDs.map((id) => genreLabel(id)).join(", ")}${anywhere ? ", anywhere" : ` in ${place}`}, from the Messer Lineup index.`
          : filtered
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
/** The published genre artifact, cached by its content-addressed name. `null` means it could not be read. */
const genreCache = new Map<string, Promise<Record<string, string[]> | null>>();

function genreKeys(url: URL): Promise<Record<string, string[]> | null> {
  const key = url.toString();
  const cached = genreCache.get(key);
  if (cached) return cached;
  const pending = (async (): Promise<Record<string, string[]> | null> => {
    try {
      const res = await fetch(url, { signal: AbortSignal.timeout(5000) });
      if (!res.ok) return null;
      const body = (await res.json()) as { format?: string; genres?: Record<string, string[]> };
      if (body.format !== "lineup-genres/1" || !body.genres) return null;
      return body.genres;
    } catch {
      return null;
    }
  })();
  genreCache.set(key, pending);
  pending.then((value) => {
    // A missing artifact is retried: the next request may find the one a publish just named.
    if (value === null) genreCache.delete(key);
  });
  return pending;
}

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
