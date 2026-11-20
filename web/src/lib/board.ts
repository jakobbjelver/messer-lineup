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
export function mineKeys(rows: Row[], tasteKeys: Set<string>): Set<string> {
  if (tasteKeys.size === 0) return new Set();
  const out = new Set<string>();
  for (const row of rows) {
    if (tasteKeys.has(Key(row.a))) out.add(row.k);
  }
  return out;
}

export function filterMine(rows: Row[], mine: Set<string>): Row[] {
  if (mine.size === 0) return rows;
  return rows.filter((row) => mine.has(row.k));
}

/** Days between two `YYYY-MM-DD` values, inclusive of both ends. */
export function daysBetween(from: string, to: string): number {
  return Math.round((dateOf(to).getTime() - dateOf(from).getTime()) / 86_400_000) + 1;
}

/** Every day of a month, in the Monday-first grid a European calendar uses. */
export function monthCells(month: string): { day: string; inMonth: boolean }[] {
  const [year, mon] = month.split("-").map(Number);
  const first = Date.UTC(year!, mon! - 1, 1, 12);
  const offset = (new Date(first).getUTCDay() + 6) % 7; // Monday = 0
  const start = first - offset * 86_400_000;
  const cells: { day: string; inMonth: boolean }[] = [];
  for (let i = 0; i < 42; i += 1) {
    const d = new Date(start + i * 86_400_000);
    cells.push({ day: d.toISOString().slice(0, 10), inMonth: d.getUTCMonth() === mon! - 1 });
  }
  // A six-week grid with a whole empty trailing week is a month that looks broken; trim it.
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
