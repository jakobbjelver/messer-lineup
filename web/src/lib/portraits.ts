/**
 * Portraits: one published file, fetched once, never blocking the board.
 *
 * A row must look right with no image at all (ADR-002's own condition), so this module never gates a
 * render. It resolves a map of `artistKey → url` after the first paint, and a component that has no
 * entry keeps its initials. A missing file is not an error — it is a build that has not published
 * portraits yet, which is a state the UI is required to survive.
 */

import { Key } from "./key";

export type Portrait = { u: string; c?: string };
export type PortraitMap = Map<string, Portrait>;

const BASE = (import.meta.env.VITE_DATA_BASE ?? "").replace(/\/$/, "");

let pending: Promise<PortraitMap> | null = null;

export function loadPortraits(): Promise<PortraitMap> {
  if (pending) return pending;
  pending = (async () => {
    try {
      const res = await fetch(`${BASE}/artists/portraits.json`, { cache: "force-cache" });
      if (!res.ok) return new Map();
      const body = (await res.json()) as Record<string, Portrait>;
      const map: PortraitMap = new Map();
      for (const [key, value] of Object.entries(body)) {
        if (value && typeof value.u === "string" && value.u) map.set(key, value);
      }
      return map;
    } catch {
      return new Map();
    }
  })();
  return pending;
}

/**
 * The two letters an avatar wears before its face arrives. Deterministic, so it never flickers.
 *
 * A leading number is a date or a count ("30 minuter om…", "1/28 NOAH GUNDERSEN"), so initials prefer
 * alphabetic words; if there are none, the number is the name and its own letters are used.
 */
export function initials(name: string): string {
  const words = name
    .replace(/[^\p{L}\p{N}\s&+/-]/gu, " ")
    .split(/[\s&+/-]+/)
    .filter((word) => word.length > 0);
  if (words.length === 0) return "··";
  const letters = words.filter((word) => /\p{L}/u.test(word));
  const pool = letters.length >= 2 ? letters : words;
  if (pool.length === 1) return pool[0]!.replace(/^\d+\s*/, "").slice(0, 2).toUpperCase() || "··";
  return (pool[0]![0]! + pool[1]![0]!).toUpperCase();
}

/** The source's own name, for a credit line: a Deezer CDN image is Deezer's, a Wikipedia article is Wikipedia's. */
export function sourceName(credit: string | undefined): string {
  const url = credit ?? "";
  if (url.includes("deezer.com")) return "Deezer";
  if (url.includes("commons.wikimedia.org")) return "Wikimedia Commons";
  if (url.includes("wikipedia.org")) return "Wikipedia";
  return "Wikimedia Commons";
}

/** Every source the published file actually uses, for the footer's one line. */
export function sourceNames(map: PortraitMap): string[] {
  const names = new Set<string>();
  for (const portrait of map.values()) names.add(sourceName(portrait.c));
  return [...names].sort();
}

/** A portrait for a display name, when the published file has one. */
export function portraitFor(map: PortraitMap, name: string): Portrait | undefined {
  return map.get(Key(name));
}
