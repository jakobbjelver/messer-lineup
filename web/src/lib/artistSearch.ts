/**
 * Artist search, through our own edge.
 *
 * MusicBrainz answers `Access-Control-Allow-Origin: *`, so a browser *could* call it directly — but a
 * second call within a second earns a `503 busy` (measured), and a combobox is exactly the client that
 * provokes that. The proxy on the Worker caches by query and serialises upstream calls, so this file
 * only has to be a well-behaved caller: debounce, abort, and never treat a failure as an error state.
 *
 * Two upstream failures reach a combobox and they are not the same thing:
 *
 *   - a **503** while MusicBrainz still reports quota left, which is a hiccup and is worth one retry
 *     (the proxy retries too, on its own side — both ends are cheap and the second is the one the reader
 *     never sees);
 *   - a **200 that is not JSON**, which is what an older runtime or a mis-proxied path answers. That used
 *     to land as `unavailable: true` with no explanation on screen, and looked exactly like "no results".
 */

import { TASTE_CAP } from "./calendar";

export type ArtistHit = {
  name: string;
  mbid: string;
  type?: string;
  country?: string;
  disambiguation?: string;
};

type Answer = { artists?: ArtistHit[]; unavailable?: boolean };

export type SearchResult = { hits: ArtistHit[]; unavailable: boolean };

/** A pause that gives up as soon as the next keystroke aborts the search. */
function settle(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(resolve, ms);
    signal.addEventListener(
      "abort",
      () => {
        window.clearTimeout(timer);
        reject(new DOMException("Aborted", "AbortError"));
      },
      { once: true },
    );
  });
}

async function once(q: string, signal: AbortSignal): Promise<SearchResult> {
  // `no-store`: the edge caches real answers, and a browser must never pin one — an unavailable answer cached for
  // six hours in localStorage of the tab is the same bug the proxy had, one layer further out.
  const res = await fetch(`/api/v1/artists/search?q=${encodeURIComponent(q)}`, { signal, cache: "no-store" });
  if (!res.ok) return { hits: [], unavailable: res.status >= 500 };
  // A JSON route that answers HTML is a missing route behind an SPA fallback, not an empty result list.
  if (!(res.headers.get("content-type") ?? "").includes("json")) return { hits: [], unavailable: true };
  const body = (await res.json()) as Answer;
  const hits = Array.isArray(body.artists) ? body.artists : [];
  return { hits, unavailable: body.unavailable === true };
}

export async function searchArtists(query: string, signal: AbortSignal): Promise<SearchResult> {
  const q = query.trim();
  if (q.length < 2) return { hits: [], unavailable: false };
  try {
    let result = await once(q, signal);
    if (result.unavailable) {
      await settle(900, signal);
      result = await once(q, signal);
    }
    return result;
  } catch (err) {
    // An abort is the user typing, not a failure.
    if (err instanceof DOMException && err.name === "AbortError") throw err;
    return { hits: [], unavailable: true };
  }
}

/** The import endpoints that need no account of ours: ListenBrainz direct, Last.fm through the edge. */
export async function importFrom(service: "listenbrainz" | "lastfm", handle: string): Promise<string[]> {
  const user = handle.trim();
  if (!user) return [];
  if (service === "listenbrainz") {
    // The list is capped by the board, not by the provider: ListenBrainz answers `count=1000` (measured 2026-11-20,
    // 200 artists in 20 KB in 1.4 s), so asking for the cap in one call is free and a second page is not needed.
    const res = await fetch(
      `https://api.listenbrainz.org/1/stats/user/${encodeURIComponent(user)}/artists?range=year&count=${TASTE_CAP}`,
    );
    if (!res.ok) {
      // 204 is an answer, not a failure: the account exists and its statistics are not computed yet.
      if (res.status === 204) return [];
      throw new Error(res.status === 404 ? "No such ListenBrainz user." : "ListenBrainz did not answer.");
    }
    const body = (await res.json()) as { payload?: { artists?: { artist_name?: string }[] } };
    return (body.payload?.artists ?? [])
      .map((row) => row.artist_name)
      .filter((name): name is string => typeof name === "string" && name.length > 0);
  }
  // The edge holds the key and the cache, so this is one call to our own origin. `no-store`: the edge caches a real
  // answer for six hours and an unavailable one for no time at all, and the tab must not add a third opinion.
  const res = await fetch(`/api/v1/taste/lastfm?user=${encodeURIComponent(user)}`, { cache: "no-store" });
  if (!res.ok) throw new Error(res.status === 404 ? "No such Last.fm user." : "Last.fm did not answer.");
  const body = (await res.json()) as { artists?: string[] };
  return (body.artists ?? []).filter((name) => typeof name === "string" && name.length > 0);
}
