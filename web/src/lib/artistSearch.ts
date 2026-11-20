/**
 * Artist search, through our own edge.
 *
 * MusicBrainz answers `Access-Control-Allow-Origin: *`, so a browser *could* call it directly — but a
 * second call within a second earns a `503 busy` (measured), and a combobox is exactly the client that
 * provokes that. The proxy on the Worker caches by query and serialises upstream calls, so this file
 * only has to be a well-behaved caller: debounce, abort, and never treat a failure as an error state.
 */

export type ArtistHit = {
  name: string;
  mbid: string;
  type?: string;
  country?: string;
  disambiguation?: string;
};

type Answer = { artists?: ArtistHit[]; unavailable?: boolean };

export type SearchResult = { hits: ArtistHit[]; unavailable: boolean };

export async function searchArtists(query: string, signal: AbortSignal): Promise<SearchResult> {
  const q = query.trim();
  if (q.length < 2) return { hits: [], unavailable: false };
  try {
    const res = await fetch(`/api/v1/artists/search?q=${encodeURIComponent(q)}`, { signal });
    if (!res.ok) return { hits: [], unavailable: res.status >= 500 };
    const body = (await res.json()) as Answer;
    const hits = Array.isArray(body.artists) ? body.artists : [];
    return { hits, unavailable: body.unavailable === true };
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
    const res = await fetch(
      `https://api.listenbrainz.org/1/stats/user/${encodeURIComponent(user)}/artists?range=year&count=50`,
    );
    if (!res.ok) throw new Error(res.status === 404 ? "No such ListenBrainz user." : "ListenBrainz did not answer.");
    const body = (await res.json()) as { payload?: { artists?: { artist_name?: string }[] } };
    return (body.payload?.artists ?? [])
      .map((row) => row.artist_name)
      .filter((name): name is string => typeof name === "string" && name.length > 0);
  }
  const res = await fetch(`/api/v1/taste/lastfm?user=${encodeURIComponent(user)}`);
  if (!res.ok) throw new Error(res.status === 404 ? "No such Last.fm user." : "Last.fm did not answer.");
  const body = (await res.json()) as { artists?: string[] };
  return (body.artists ?? []).filter((name) => typeof name === "string" && name.length > 0);
}
