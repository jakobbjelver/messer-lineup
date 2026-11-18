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

function base64url(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
