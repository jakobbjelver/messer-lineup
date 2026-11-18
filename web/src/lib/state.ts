/**
 * What the board remembers, and what it writes down.
 *
 * Two different kinds of state, deliberately kept apart:
 *
 *   - **The URL** carries everything that makes a board shareable: the town, the taste list, and a travel window.
 *     Opening someone else's link gives you their board, not yours.
 *   - **localStorage** carries only what is about the *visitor*: the theme, and which shows were already on the
 *     board last time. Nothing about it leaves the browser, and clearing it costs nothing but the "new" markers.
 *
 * A row has no "added" timestamp — the nine-key contract has no room for one, and inventing one would make the
 * board lie about when it was built. So "new since last visit" means exactly what it can: *this show was not on
 * the board when you last looked*.
 */

import { addDays, dayOf, isoDay, type Manifest } from "./indexData";

const SEEN_KEY = "lineup.seen";
const VISIT_KEY = "lineup.visited";
/** A last-visit memory older than this is not a comparison, it is a different season. */
const MAX_MEMORY_AGE_DAYS = 30;
/** Enough keys for a heavy window without turning localStorage into a database. */
const MAX_SEEN = 6000;

export type Window = { from: string; to: string };

/** The window a manifest publishes, as `YYYY-MM-DD`. */
export function publishedWindow(manifest: Manifest): Window {
  return { from: manifest.window.from, to: manifest.window.to };
}

/** A window from the URL, clamped to what is published. Anything unreadable is ignored, never guessed. */
export function windowFromURL(search: string, published: Window): Window {
  const params = new URLSearchParams(search);
  const from = normaliseDay(params.get("from"));
  const to = normaliseDay(params.get("to"));
  if (!from && !to) return published;
  const start = from && from > published.from ? from : published.from;
  const end = to && to < published.to ? to : published.to;
  // A reversed or empty range is not a window; the published one is the answer.
  if (start > end) return published;
  return { from: start, to: end };
}

/** Writes the window into the URL, dropping the parameters when they match what is published. */
export function windowToURL(window: Window, published: Window, current: string): string {
  const url = new URL(current);
  if (window.from === published.from) url.searchParams.delete("from");
  else url.searchParams.set("from", window.from);

  // `to` is stored as a day the visitor *includes*, which is more forgiving than an exclusive bound in a URL a
  // person types by hand.
  if (window.to === published.to) url.searchParams.delete("to");
  else url.searchParams.set("to", window.to);

  return `${url.pathname}${url.search}${url.hash}`;
}

function normaliseDay(value: string | null): string | null {
  if (!value) return null;
  const trimmed = value.trim();
  if (!/^\d{4}-\d{2}-\d{2}$/.test(trimmed)) return null;
  const parsed = new Date(`${trimmed}T12:00:00Z`);
  if (Number.isNaN(parsed.getTime())) return null;
  return isoDay(parsed);
}

/* ── the visitor's memory ─────────────────────────────────────────────────────────────────────────────── */

type Memory = { seen: string[]; visited: string };

function readMemory(): Memory | null {
  try {
    const raw = localStorage.getItem(SEEN_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<Memory>;
    if (!Array.isArray(parsed.seen)) return null;
    return { seen: parsed.seen.filter((k): k is string => typeof k === "string"), visited: String(parsed.visited ?? "") };
  } catch {
    // A browser with storage disabled, or a value from an older build: no memory is a valid state.
    return null;
  }
}

/** The keys this browser saw last time, or an empty set when there is nothing to compare against. */
export function previouslySeen(today: string): Set<string> {
  const memory = readMemory();
  if (!memory || !memory.visited) return new Set();
  const visited = memory.visited.slice(0, 10);
  // Only compare with a recent visit: after a month the whole board is different and every row would be "new".
  if (visited && addDays(visited, MAX_MEMORY_AGE_DAYS) < today) return new Set();
  return new Set(memory.seen);
}

/** Records this render as a visit, so the next one can tell what changed. */
export function rememberVisit(keys: string[], today: string): void {
  try {
    localStorage.setItem(SEEN_KEY, JSON.stringify({ seen: keys.slice(0, MAX_SEEN), visited: today } satisfies Memory));
    localStorage.setItem(VISIT_KEY, today);
  } catch {
    /* storage refused: the board still works, it just never marks anything new */
  }
}

/** The day the index was built: the board's "today", because the miner's clock is the one that saw these rows. */
export function boardDay(manifest: Manifest): string {
  return dayOf(manifest.built_at);
}
