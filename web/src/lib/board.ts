/**
 * The board as a pure function: group by day, then narrow.
 *
 * Neither of these reads a clock. The board is a calendar over a published window, and every test of
 * it supplies its own "today" — a selection that calls `Date.now()` internally can only be asserted
 * against the day it runs on.
 */

import { Key } from "./key";
import { dateOf, dayOf, type Row } from "./indexData";

export type DayGroup = { day: string; rows: Row[] };

/** Rows arrive sorted by start; a group keeps that order and never re-sorts. */
export function groupByDay(rows: Row[]): DayGroup[] {
  const groups: DayGroup[] = [];
  let current: DayGroup | null = null;
  for (const row of rows) {
    const day = dayOf(row.d);
    if (!current || current.day !== day) {
      current = { day, rows: [] };
      groups.push(current);
    }
    current.rows.push(row);
  }
  return groups;
}

/** Which rows are the reader's. A row is yours when its artist's identity key is in the list. */
export function mineKeys(rows: Row[], tasteKeys: Set<string>, genreKeys: Set<string> = new Set()): Set<string> {
  if (tasteKeys.size === 0 && genreKeys.size === 0) return new Set();
  const out = new Set<string>();
  for (const row of rows) {
    const key = Key(row.a);
    // A genre is a membership set, not a second filter: the two are a union, and one row can arrive by both roads.
    if (tasteKeys.has(key) || genreKeys.has(key)) out.add(row.k);
  }
  return out;
}

export function filterMine(rows: Row[], mine: Set<string>): Row[] {
  // No artists is an empty answer, not "show everything": the reader asked for their list, and a list they have
  // not built yet is a prompt to build it.
  return rows.filter((row) => mine.has(row.k));
}

/** Days between two `YYYY-MM-DD` values, inclusive of both ends. */
export function daysBetween(from: string, to: string): number {
  return Math.round((dateOf(to).getTime() - dateOf(from).getTime()) / 86_400_000) + 1;
}

/**
 * The month grid, Monday-first, as six rows at most.
 *
 * `anchor` is the reader's today. For the **current** month the grid starts on the Monday of the anchor's week
 * instead of the 1st, so opening the calendar shows this week and five weeks of future rather than a month that
 * is mostly gone; every other month starts on its own first week. Cells before the shown month are blank — a
 * calendar that opens on the 26th of last month reads as a month out of date.
 */
export function monthCells(month: string, anchor?: string): { day: string; inMonth: boolean; blank: boolean }[] {
  const [year, mon] = month.split("-").map(Number);
  const firstMs = Date.UTC(year!, mon! - 1, 1, 12);
  const firstIso = `${month}-01`;
  const sameMonth = (anchor ?? "").slice(0, 7) === month;
  const anchorMs = sameMonth ? Date.parse(`${anchor}T12:00:00Z`) : Number.NaN;
  const base = Number.isFinite(anchorMs) && anchorMs > firstMs ? anchorMs : firstMs;
  const offset = (new Date(base).getUTCDay() + 6) % 7; // Monday = 0
  const start = base - offset * 86_400_000;

  const cells: { day: string; inMonth: boolean; blank: boolean }[] = [];
  for (let i = 0; i < 42; i += 1) {
    const d = new Date(start + i * 86_400_000);
    const day = d.toISOString().slice(0, 10);
    cells.push({ day, inMonth: d.getUTCMonth() === mon! - 1, blank: day < firstIso });
  }
  // A trailing row with nothing in the shown month is a month that looks broken; five rows is still a grid.
  while (cells.length > 35 && cells.slice(35).every((cell) => !cell.inMonth)) cells.splice(35);
  return cells;
}

export function monthOf(day: string): string {
  return day.slice(0, 7);
}

/** Today in the reader's own time zone — a calendar's "today" is where the person is, not where the miner is. */
export function localDay(date = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/** `2026-11` → the next or previous month, without a Date round trip through local time. */
export function shiftMonth(month: string, delta: number): string {
  const [year, mon] = month.split("-").map(Number);
  return new Date(Date.UTC(year!, mon! - 1 + delta, 1, 12)).toISOString().slice(0, 7);
}
