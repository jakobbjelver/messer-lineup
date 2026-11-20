/**
 * What the browser remembers about the reader.
 *
 * Four facts, four keys, one shape: the town they chose, the artists they chose, the view they last
 * used and whether they have seen the gate. Everything else (a distance, a fix, an open panel) is
 * memory-only by design — a stored guess is indistinguishable from a stored choice.
 *
 * A browser that refuses storage gets a working app that simply forgets: every read falls back to a
 * default and every write is swallowed.
 */

import type { TasteEntry } from "./taste";

export type View = "list" | "calendar";

const TOWN_KEY = "lineup.town";
const TASTE_KEY = "lineup.taste";
const VIEW_KEY = "lineup.view";
const ONBOARDED_KEY = "lineup.onboarded";

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
    return parsed.filter(
      (entry): entry is TasteEntry =>
        !!entry &&
        typeof entry === "object" &&
        typeof (entry as TasteEntry).key === "string" &&
        typeof (entry as TasteEntry).name === "string",
    );
  } catch {
    return [];
  }
}

export function writeTaste(entries: TasteEntry[]): void {
  write(TASTE_KEY, entries.length ? JSON.stringify(entries.slice(0, 400)) : null);
}

export function readOnboarded(): boolean {
  return read(ONBOARDED_KEY) === "1";
}

export function writeOnboarded(): void {
  write(ONBOARDED_KEY, "1");
}
