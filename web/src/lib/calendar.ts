/**
 * The feed, as the board builds it.
 *
 * One codec, two consumers: the Worker validates a token with `src/calendar/spec.ts`, and this file encodes the same
 * shape with the same function. Importing it rather than re-implementing it is the whole point — a button that writes
 * a token the feed will refuse with a `400` is a broken control, and the only place that bug could be discovered is
 * the subscriber's own phone, days later.
 *
 * Two rules the URL follows:
 *
 *   - **No window.** A feed that carries `f`/`o` dies on its last day: the client keeps polling an empty calendar for
 *     ever. Absent them, the feed follows the published window, which the nightly publish moves.
 *   - **The artist list is spelled out.** `a` is the snapshot, and it is also the fallback for a live source, so a
 *     feed is never worse than the day it was issued.
 */

import { ANYWHERE, encodeSpec, TASTE_CAP, type CalendarSource, type CalendarSpec } from "../../../src/calendar/spec";
import type { TasteEntry } from "./taste";

export { ANYWHERE, TASTE_CAP };
export type { CalendarSource, CalendarSpec };

/** What a subscription is for: a scope, the artists, and optionally where to follow them from. */
export type FeedBoard = {
  /** A town id, or `anywhere`. */
  scope: string;
  /** The reader's list, in order. */
  artists: TasteEntry[];
  /** A live list to prefer, with `artists` as the snapshot it falls back to. */
  source?: CalendarSource;
  /** Genre ids. `a` and `g` are a union on the feed, so a genre-only calendar is a valid board. */
  genres?: string[];
  /** Ask the calendar client for a day-before alarm. A v2 field, so its presence is what moves the token's version. */
  reminder?: boolean;
};

/**
 * The token for a board. Throws only when the list is over the cap, which the picker already prevents — and it throws
 * rather than writing a link the feed would refuse.
 */
export function tokenFor(board: FeedBoard): string {
  const spec: CalendarSpec = {
    // v2 carries `r` and `g`; a board without either stays v1 so issued links keep their shape.
    v: board.reminder || (board.genres?.length ?? 0) > 0 ? 2 : 1,
    t: board.scope,
    ...(board.artists.length > 0 ? { a: board.artists.map((entry) => entry.key).slice(0, TASTE_CAP) } : {}),
    ...(board.genres && board.genres.length > 0 ? { g: board.genres.slice(0, 6) } : {}),
    ...(board.source ? { s: board.source } : {}),
    ...(board.reminder ? { r: 1 as const } : {}),
  };
  return encodeSpec(spec);
}

export function feedPath(token: string): string {
  return `/calendar/${token}.ics`;
}

/** The address a calendar app fetches. */
export function feedUrl(origin: string, token: string): string {
  return `${origin.replace(/\/$/, "")}${feedPath(token)}`;
}

/**
 * The address that opens a subscription sheet. `webcal:` is the scheme Apple's Calendar registers for exactly this;
 * Google has no handler, which is why the control also copies the `https:` form.
 */
export function webcalUrl(origin: string, token: string): string {
  return feedUrl(origin, token).replace(/^https?:/, "webcal:");
}

/** How many artists the feed will carry — the same number the picker stops at. */
export const FEED_ARTIST_CAP = TASTE_CAP;
