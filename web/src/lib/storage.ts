/**
 * What the browser remembers about the reader.
 *
 * Five facts, five keys, one shape: the town they chose, the artists they chose, the view they last used, whether they
 * have seen the gate, and the feed token the subscribe control last issued. Everything else (a distance, a fix, an
 * open panel) is memory-only by design — a stored guess is indistinguishable from a stored choice.
 *
 * The remembered token is not state about the reader's taste; it is a bookmark of our own output, kept so the control
 * can say "update subscription" instead of handing them a second calendar that duplicates the first.
 *
 * A browser that refuses storage gets a working app that simply forgets: every read falls back to a default and every
 * write is swallowed.
 */

import { TASTE_CAP, type CalendarSource } from "./calendar";
import type { TasteEntry } from "./taste";

export type View = "list" | "calendar";

/** Where a list came from, when it came from an import: the source and the exact keys it produced. */
export type TasteImport = CalendarSource & { keys: string[] };

const TOWN_KEY = "lineup.town";
const TASTE_KEY = "lineup.taste";
const VIEW_KEY = "lineup.view";
const ONBOARDED_KEY = "lineup.onboarded";
const IMPORT_KEY = "lineup.taste.import";
const FEED_KEY = "lineup.calendar";

function read(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string | null): void {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    /* storage refused: the app still works, it just forgets */
  }
}

export function readTown(): string | null {
  const value = read(TOWN_KEY);
  return value !== null && value !== "" ? value : null;
}

export function writeTown(id: string | null): void {
  write(TOWN_KEY, id);
}

export function readView(): View {
  return read(VIEW_KEY) === "calendar" ? "calendar" : "list";
}

export function writeView(view: View): void {
  write(VIEW_KEY, view === "calendar" ? "calendar" : null);
}

export function readTaste(): TasteEntry[] {
  const raw = read(TASTE_KEY);
  if (!raw) return [];
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed
      .filter(
        (entry): entry is TasteEntry =>
          !!entry &&
          typeof entry === "object" &&
          typeof (entry as TasteEntry).key === "string" &&
          typeof (entry as TasteEntry).name === "string",
      )
      .slice(0, TASTE_CAP);
  } catch {
    return [];
  }
}

export function writeTaste(entries: TasteEntry[]): void {
  // One cap for the picker, the storage and the feed: a list the board can hold but the link cannot carry is a list
  // that quietly loses its tail on the next visit.
  write(TASTE_KEY, entries.length ? JSON.stringify(entries.slice(0, TASTE_CAP)) : null);
}

/**
 * The provenance of a list that was imported. The feed follows a source only while the list is still exactly what the
 * import produced — one edit and the reader has said "this is my list now", so the snapshot in the token is the truth
 * again. That comparison is the reason the keys are stored with the handle.
 */
export function readImport(): TasteImport | null {
  const raw = read(IMPORT_KEY);
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as Partial<TasteImport>;
    if (parsed.k !== "listenbrainz" || typeof parsed.u !== "string" || !Array.isArray(parsed.keys)) return null;
    return { k: "listenbrainz", u: parsed.u, keys: parsed.keys.filter((key): key is string => typeof key === "string") };
  } catch {
    return null;
  }
}

export function writeImport(source: TasteImport | null): void {
  write(IMPORT_KEY, source ? JSON.stringify(source) : null);
}

/** The source to follow, or nothing when the list has been edited since the import. */
export function followingSource(entries: TasteEntry[]): CalendarSource | null {
  const imported = readImport();
  if (!imported) return null;
  const now = entries.map((entry) => entry.key);
  if (now.length !== imported.keys.length) return null;
  const known = new Set(imported.keys);
  if (!now.every((key) => known.has(key))) return null;
  return { k: imported.k, u: imported.u };
}

/** The token the subscribe control last issued, so it can offer to replace that subscription rather than add one. */
export function readFeed(): string | null {
  const value = read(FEED_KEY);
  return value !== null && value !== "" ? value : null;
}

export function writeFeed(token: string | null): void {
  write(FEED_KEY, token);
}

export function readOnboarded(): boolean {
  return read(ONBOARDED_KEY) === "1";
}

export function writeOnboarded(): void {
  write(ONBOARDED_KEY, "1");
}
