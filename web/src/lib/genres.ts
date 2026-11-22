/**
 * The sixteen words, and the map from them to artists.
 *
 * The vocabulary itself lives in `src/calendar/genres.ts`, beside the token that carries it, so the picker and the
 * Worker's calendar description cannot drift. This module is the board's side: the URL parameter, the storage, and the
 * published artifact.
 */

import { GENRES, GENRE_CAP, genreLabel, type GenreId } from "../../../src/calendar/genres";

export { GENRES, GENRE_CAP, genreLabel };
export type { GenreId };

const BASE = (import.meta.env.VITE_DATA_BASE ?? "").replace(/\/$/, "");

const KNOWN = new Set<string>(GENRES.map((genre) => genre.id));

export function isGenre(id: string): id is GenreId {
  return KNOWN.has(id as GenreId);
}

/** `?g=metal,punk`: unknown ids are dropped, order is preserved, the cap is applied. */
export function readGenreParam(search: string): GenreId[] {
  const raw = new URLSearchParams(search).get("g");
  if (!raw) return [];
  const out: GenreId[] = [];
  for (const value of raw.split(",")) {
    const id = value.trim();
    if (!id || !KNOWN.has(id as GenreId) || out.includes(id as GenreId)) continue;
    out.push(id as GenreId);
  }
  return out.slice(0, GENRE_CAP);
}

/** Writes `g` and drops it when empty, so clearing genres leaves no residue in a shared link. */
export function writeGenreParam(genres: readonly string[], current: string): string {
  const url = new URL(current);
  if (genres.length === 0) url.searchParams.delete("g");
  else url.searchParams.set("g", genres.slice(0, GENRE_CAP).join(","));
  return `${url.pathname}${url.search}${url.hash}`;
}

export function toggleGenre(list: readonly GenreId[], id: GenreId): GenreId[] {
  if (list.includes(id)) return list.filter((entry) => entry !== id);
  if (list.length >= GENRE_CAP) return [...list];
  return [...list, id];
}

/** The published map, both directions. An artist with no entry has no genre; that is an answer, not a hole. */
export type GenreMap = {
  /** artist key → up to three genre ids. */
  artist: ReadonlyMap<string, readonly string[]>;
  /** genre id → artist keys. */
  genres: ReadonlyMap<string, readonly string[]>;
};

export const EMPTY_GENRE_MAP: GenreMap = { artist: new Map(), genres: new Map() };

const cache = new Map<string, Promise<GenreMap>>();

/**
 * Reads `artists/genres-<sha>.json` as the manifest named it. A failure is an empty map, never a broken board: genres
 * are an enhancement, and a build that has not published them yet must still show shows.
 */
export function loadGenres(file: string | undefined): Promise<GenreMap> {
  if (!file) return Promise.resolve(EMPTY_GENRE_MAP);
  const cached = cache.get(file);
  if (cached) return cached;
  const pending = (async (): Promise<GenreMap> => {
    try {
      const res = await fetch(`${BASE}/${file.replace(/^\//, "")}`, { cache: "force-cache" });
      if (!res.ok) return EMPTY_GENRE_MAP;
      const body = (await res.json()) as {
        format?: string;
        artist?: Record<string, string[]>;
        genres?: Record<string, string[]>;
      };
      if (body.format !== "lineup-genres/1") return EMPTY_GENRE_MAP;
      const artist = new Map<string, readonly string[]>();
      for (const [key, ids] of Object.entries(body.artist ?? {})) {
        const known = ids.filter((id) => KNOWN.has(id as GenreId));
        if (known.length) artist.set(key, known);
      }
      const genres = new Map<string, readonly string[]>();
      for (const [id, keys] of Object.entries(body.genres ?? {})) {
        if (KNOWN.has(id as GenreId) && keys.length) genres.set(id, keys);
      }
      return { artist, genres };
    } catch {
      return EMPTY_GENRE_MAP;
    }
  })();
  cache.set(file, pending);
  pending.then((map) => {
    // A missing artifact is not an error: the next visit may find one.
    if (map.artist.size === 0) cache.delete(file);
  });
  return pending;
}

/** Every artist key that plays one of the chosen genres. Unknown ids contribute nothing. */
export function keysForGenres(map: GenreMap, genres: readonly string[]): Set<string> {
  const out = new Set<string>();
  for (const genre of genres) {
    for (const key of map.genres.get(genre) ?? []) out.add(key);
  }
  return out;
}
