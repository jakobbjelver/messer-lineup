/**
 * The calendar link the board shows.
 *
 * The token is base64url of `{v,t,f,o,a?}` — the same shape `src/calendar/spec.ts` decodes in the Worker. It is written
 * here as a five-line encoder rather than imported across the `web/` boundary because the app's build root is `web/`,
 * and a token that is wrong in a way the decoder rejects is caught by one request, not by a type.
 *
 * Nothing in it is secret: it names a slice of the public index, so the link can be shared, bookmarked and pasted into
 * any calendar client without an account anywhere.
 */

import { Key } from "./key";

export type CalendarLinks = { token: string; https: string; webcal: string };

/** The feed for the board as it is on screen: this town, this window, this taste list. */
export function calendarLink(
  town: string,
  window: { from: string; to: string },
  artistNames: string[],
): CalendarLinks {
  const spec: { v: 1; t: string; f: string; o: string; a?: string[] } = {
    v: 1,
    t: town,
    f: window.from,
    o: window.to,
  };
  const keys = [...new Set(artistNames.map((name) => Key(name)).filter(Boolean))];
  if (keys.length) spec.a = keys;

  const token = base64url(new TextEncoder().encode(JSON.stringify(spec)));
  const origin = typeof location === "undefined" ? "" : location.origin;
  const https = `${origin}/calendar/${token}.ics`;
  return { token, https, webcal: https.replace(/^https?:\/\//, "webcal://") };
}

/**
 * One show, as a calendar link.
 *
 * The row is nine short strings, so the whole event fits in the token and `/event/<token>.ics` answers without reading
 * anything. The token is unsigned for the same reason the feed's is: it grants nothing but a slice of the public board.
 */
export function eventLink(row: { k: string; a: string; v: string; d: string; u: string }, townLabel: string): CalendarLinks {
  const spec = { v: 1, k: row.k, a: row.a, v2: row.v, d: row.d, u: row.u, n: townLabel };
  const token = base64url(new TextEncoder().encode(JSON.stringify(spec)));
  const origin = typeof location === "undefined" ? "" : location.origin;
  const https = `${origin}/event/${token}.ics`;
  return { token, https, webcal: https.replace(/^https?:\/\//, "webcal://") };
}

function base64url(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
